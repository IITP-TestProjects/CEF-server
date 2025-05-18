package main

import (
	"context"
	"log"
	"sync"

	pb "test-server/proto_interface"

	"github.com/bford/golang-x-crypto/ed25519"
)

var recvPartPubKey []ed25519.PublicKey

//network.go에는 grpc RPC구현에 관련된 내용만
//이외 backend logic은 타 파일에 구현

type meshSrv struct {
	pb.UnimplementedMeshServer
	mu   sync.RWMutex
	subs map[string]chan *pb.FinalizedCommittee // 노드ID → 스트림 송신 채널
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

// RPC에서 호출
func (m *meshSrv) Publish(_ context.Context, p *pb.FinalizedCommittee) (*pb.Ack, error) {
	m.broadcast(p)
	return &pb.Ack{Ok: true}, nil
}

func (m *meshSrv) RequestCommittee(_ context.Context, cci *pb.CommitteeCandidateInfo) (*pb.Ack, error) {
	// 노드ID에 해당하는 커밋리스트를 반환
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 노드ID에 해당하는 커밋리스트를 찾음
	if nodeId, ok := m.subs[cci.NodeId]; ok {
		log.Println("node", nodeId, "requested committee")
		// 커밋리스트를 생성하여 반환
		return &pb.Ack{Ok: true}, nil
	}
	return nil, nil
}
