package main

import (
	"context"
	"log"
	"sync"

	pb "test-server/proto_interface"
)

// network.go에는 grpc RPC구현에 관련된 내용만
// 이외 backend-like logic은 타 파일에 구현
type meshSrv struct {
	pb.UnimplementedMeshServer
	mu   sync.RWMutex
	subs map[string]chan *pb.FinalizedCommittee // 노드ID → 스트림 송신 채널

	committeeCandidates map[uint64][]*pb.CommitteeCandidateInfo // 후보자 정보
}

func newMeshSrv() *meshSrv {
	return &meshSrv{
		subs: make(map[string]chan *pb.FinalizedCommittee),
	}
}

// JoinNetwork: 클라이언트 → NodeAccount, 서버 → 지속적 Payload 스트림
func (m *meshSrv) JoinNetwork(acc *pb.NodeAccount, stream pb.Mesh_JoinNetworkServer) error {
	nodeID := acc.NodeId
	//pubKey := acc.PublicKey //=> 차후 Schnorr 구현 시 사용
	ch := make(chan *pb.FinalizedCommittee, 32)

	// 구독자로 등록
	m.mu.Lock()
	m.subs[nodeID] = ch
	m.mu.Unlock()

	log.Printf("node %s joined", nodeID)

	// 스트림 종료 시 정리
	defer func() {
		m.mu.Lock()
		delete(m.subs, nodeID)
		m.mu.Unlock()
		close(ch)
		log.Printf("node %s left", nodeID)
	}()

	// 채널에 오는 메시지를 계속 스트림으로 밀어줌
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case msg := <-ch:
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

// graceful-shutdown을 위한 함수, 탈퇴처리
func (m *meshSrv) LeaveNetwork(ctx context.Context, acc *pb.NodeAccount) (*pb.Ack, error) {
	m.mu.Lock()
	if ch, ok := m.subs[acc.NodeId]; ok {
		delete(m.subs, acc.NodeId)
		close(ch) // 열린 스트림 정리
	}
	m.mu.Unlock()
	return &pb.Ack{Ok: true}, nil
}

// ─ fan-out 로직을 별도 함수로 뺌
func (m *meshSrv) broadcast(msg *pb.FinalizedCommittee) {
	m.mu.RLock()
	for _, ch := range m.subs {
		/* if id == msg.CommitteeList[ch].NodeId { // 필요하면 자기 자신 제외
			continue
		} */
		select { // 버퍼 풀일 땐 드롭
		case ch <- msg:
		default:
		}
	}
	m.mu.RUnlock()
}

// RPC에서 호출 -> 사실 broadcast가 있으므로 해당 rpc는 굳이 필요없다.
// 단방향 broadcast만이 필요함.
func (m *meshSrv) Publish(_ context.Context, p *pb.FinalizedCommittee) (*pb.Ack, error) {
	m.broadcast(p)
	return &pb.Ack{Ok: true}, nil
}

func (m *meshSrv) RequestCommittee(_ context.Context, cci *pb.CommitteeCandidateInfo) (*pb.Ack, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	//cci 데이터를 일일이 저장하고, 4개 노드가 모이면 다음작업을 실행하도록

	m.appendCandidate(cci)   // 후보자 등록
	m.tryFinalize(cci.Round) // 4개 모였으면 처리
	m.gcOldRounds(cci.Round) // GC

	return &pb.Ack{Ok: true}, nil
}
