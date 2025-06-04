package main

import (
	"context"
	"log"
	"sync"

	pb "test-server/proto_interface"

	"github.com/bford/golang-x-crypto/ed25519/cosi"
)

// network.go에는 grpc RPC구현에 관련된 내용만
// 이외 backend-like logic은 타 파일에 구현
type meshSrv struct {
	pb.UnimplementedMeshServer
	mu   sync.RWMutex
	subs map[string]chan *pb.FinalizedCommittee // 노드ID → 스트림 송신 채널

	committeeCandidates map[uint64][]*pb.CommitteeCandidateInfo // 후보자 정보
	commitData          map[uint64][]*pb.CommitData             // 커미티 정보
	cosigners           *cosi.Cosigners                         // cosi 집계용, requestCommittee호출 시 반드시 초기화
}

var (
	nodeNumber int
	nodeMu     sync.Mutex // 노드 개수 관리용 뮤텍스
)

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
	//m.mu.RLock() -> tryFinalize에서만 사용되므로, 락 사용시 deadlock발생
	//defer m.mu.RUnlock()
	for _, ch := range m.subs {
		/* if id == msg.CommitteeList[ch].NodeId { // 필요하면 자기 자신 제외
			continue
		} */
		select { // 버퍼 풀일 땐 드롭
		case ch <- msg:
		default:
		}
	}
}

// Request Committee 요청을 받는다는 것은: 커미티를 새로 생성해야 한다.
// -> 커미티의 개수가 바뀐다는 이야기이므로, 저장되어 있던 커미티 개수 정보를 초기화.
func (m *meshSrv) RequestCommittee(_ context.Context, cci *pb.CommitteeCandidateInfo) (*pb.Ack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cosigners = nil // 새로운 커미티 선정을 시작하므로 cosigners 초기화
	//cci 데이터를 일일이 저장하고, 10개(현재 고정값) 노드가 모이면 리더 및 커미티 선정하고 반환

	m.appendCandidate(cci)   // 후보자 등록
	m.tryFinalize(cci.Round) // 4개 모였으면 처리
	m.gcOldRounds(cci.Round) // Garbage Collection(window size: gcRoundWindow)

	return &pb.Ack{Ok: true}, nil
}

// 해당 RPC는 커미티가 선정된 상태에서 서명을 위해 호출되므로,
// 저장되어 있는 커미티 개수 정보를 불러와 이것을 기반으로 동작하면 된다.
func (m *meshSrv) RequestAggregatedCommit(
	_ context.Context, cd *pb.CommitData) (*pb.Ack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	log.Println("get Request Aggregate Commit nodenum:", nodeNumber)
	if m.commitData == nil {
		m.commitData = make(map[uint64][]*pb.CommitData)
	}

	m.commitData[cd.Round] = append(m.commitData[cd.Round], cd)

	nodeMu.Lock()
	if len(m.commitData[cd.Round]) < nodeNumber {
		nodeMu.Unlock()
		return &pb.Ack{Ok: true}, nil
	} else {
		nodeMu.Unlock()
	}

	var recvPartCommit []cosi.Commitment
	for _, c := range m.commitData[cd.Round] {
		recvPartCommit = append(recvPartCommit, c.Commit)
	}

	aggCommit := m.cosigners.AggregateCommit(recvPartCommit)
	m.broadcast(&pb.FinalizedCommittee{
		Round:            cd.Round,
		AggregatedCommit: aggCommit,
	})

	m.commitData[cd.Round] = nil   //
	delete(m.commitData, cd.Round) // 메모리 절약을 위해 해당 라운드 커미티 정보 초기화
	log.Println("end Request Aggregate Commit")
	return &pb.Ack{Ok: true}, nil
}
