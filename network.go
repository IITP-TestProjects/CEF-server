package main

import (
	"context"
	"log"
	"sync"
	"time"

	"test-server/golang-x-crypto/ed25519/cosi"
	pb "test-server/proto_interface"
	pv "test-server/proto_verify"
)

// network.go에는 grpc RPC구현에 관련된 내용만
// 이외 backend-like logic은 타 파일에 구현
type meshSrv struct {
	pb.UnimplementedMeshServer
	mu        sync.RWMutex
	subs      map[string]chan *pb.FinalizedCommittee // 노드ID → 스트림 송신 채널
	processed map[uint64]bool
	timers    map[uint64]*time.Timer

	committeeCandidates map[uint64][]*pb.CommitteeCandidateInfo // 후보자 정보
	commitData          map[uint64][]*pb.CommitData             // 커미티 정보
	cosigners           *cosi.Cosigners                         // cosi 집계용, requestCommittee호출 시 반드시 초기화

	verifyCli    pv.CommitteeServiceClient // verify server 클라이언트
	verifyCommCh map[uint64]chan *pv.CommitteeInfo
}

var (
	nodeNumber int
	nodeMu     sync.Mutex // 노드 개수 관리용 뮤텍스
)

func newMeshSrv(verifyCli pv.CommitteeServiceClient) *meshSrv {
	if verifyCli == nil {
		log.Println("verifyCli is nil")
		return nil
	}
	return &meshSrv{
		subs:      make(map[string]chan *pb.FinalizedCommittee),
		verifyCli: verifyCli,
	}
}

// JoinNetwork: 클라이언트 → NodeAccount, 서버 → 지속적 Payload 스트림
// JoinNetwork 데이터 수신할 때, mongoDB사용해서 데이터 추가해야함.
func (m *meshSrv) JoinNetwork(
	acc *pb.CommitteeCandidateInfo, stream pb.Mesh_JoinNetworkServer) error {
	nodeID := acc.NodeId
	//pubKey := acc.PublicKey //=> 차후 Schnorr 구현 시 사용
	ch := make(chan *pb.FinalizedCommittee, 100)

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
		m.processed = make(map[uint64]bool)
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

// graceful-shutdown을 위한 함수, 탈퇴처리 => 현재 사용하지 않음
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
		select { // 버퍼 풀일 땐 드롭
		case ch <- msg:
		default:
		}
	}
}

// Request Committee 요청을 받는다는 것은: 커미티를 새로 생성해야 한다.
// -> 커미티의 개수가 바뀐다는 이야기이므로, 저장되어 있던 커미티 개수 정보를 초기화.
// JoinNetwork와 마찬가지로 mongoDB에 데이터 추가 ->
// threshold round도달 시 새롭게 커미티 선정요청 들어올 때는 덮어쓰기를 하는게 맞을지...?
// 아니면 몇회차 커미티 요청~ 식으로 매번 데이터를 append? 하는것이 맞을지?
func (m *meshSrv) RequestCommittee(_ context.Context, cci *pb.CommitteeCandidateInfo) (*pb.Ack, error) {
	m.cosigners = nil // 새로운 커미티 선정을 시작하므로 cosigners 초기화
	//cci 데이터를 일일이 저장하고, 10개(현재 고정값) 노드가 모이면 리더 및 커미티 선정하고 반환

	//appendCandidate에서 false가 반환되면 이미 해당 라운드는 종료되었다는 뜻
	if !m.appendCandidate(cci) {
		return &pb.Ack{Ok: false}, nil
	} // 후보자 등록
	//m.tryFinalize(cci.Round) // 4개 모였으면 처리
	m.gcOldRounds(cci.Round) // Garbage Collection(window size: gcRoundWindow)

	return &pb.Ack{Ok: true}, nil
}

// 해당 RPC는 커미티가 선정된 상태에서 서명을 위해 호출되므로,
// 저장되어 있는 커미티 개수 정보를 불러와 이것을 기반으로 동작하면 된다.
// 이미 커미티가 설정된 경우, 정해진 개수 이외의 요청이 들어오는 경우 새롭게 커미티 요청을 받아야하므로
// 이것을 알리는 코드를 추가해야한다.
func (m *meshSrv) RequestAggregatedCommit(
	_ context.Context, cd *pb.CommitData) (*pb.Ack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

	// 커미티 구성 후 매 라운드에서 필요한 최소정보
	// (round, aggCommit)만 채워서 브로드캐스트
	m.broadcast(&pb.FinalizedCommittee{
		Round: cd.Round,
		//Channel: "Audit Chain",
		AggregatedCommit: aggCommit,
	})

	m.commitData[cd.Round] = nil
	delete(m.commitData, cd.Round) // 메모리 절약을 위해 해당 라운드 커미티 정보 초기화
	return &pb.Ack{Ok: true}, nil
}
