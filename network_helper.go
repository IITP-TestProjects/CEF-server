package main

import (
	"log"
	pb "test-server/proto_interface"
	"time"

	"test-server/golang-x-crypto/ed25519"
	"test-server/golang-x-crypto/ed25519/cosi"
)

const maxWait = 10 * time.Second

func (m *meshSrv) initMaps() {
	if m.processed == nil {
		m.processed = make(map[uint64]bool)
	}

	if m.timers == nil {
		m.timers = make(map[uint64]*time.Timer)
	}

	if m.committeeCandidates == nil {
		m.committeeCandidates = make(map[uint64][]*pb.CommitteeCandidateInfo)
	}
}

// 후보자정보 등록 m.mu.Lock()으로 들어온 함수
func (m *meshSrv) appendCandidate(cci *pb.CommitteeCandidateInfo) bool {
	m.initMaps()

	if m.processed[cci.Round] {
		return false
	}

	r := cci.Round
	m.committeeCandidates[r] = append(m.committeeCandidates[r], cci)

	if _, ok := m.timers[r]; !ok {
		m.timers[r] = time.AfterFunc(maxWait, func() {
			log.Println("timeout! begin to make commmittee!")
			m.tryFinalize(r)
		})
	}

	if len(m.committeeCandidates[r]) >= threshold {
		m.timers[cci.Round].Stop()
		go m.tryFinalize(r)
	}
	return true
}

// m.mu.Lock()으로 들어온 함수
func (m *meshSrv) tryFinalize(round uint64) {
	m.processed[round] = true
	delete(m.timers, round)

	cands := m.committeeCandidates[round]

	// ① metric 기반 위원·프라이머리 선정 (별도 RPC 호출)
	// MCNL 협의 필요한 부분
	// selectPrimaryAndCommittee(cands) ...
	// 커미티 정보를 받았을 때 개수를 전역변수에 저장.
	nodeMu.Lock()
	nodeNumber = len(cands)
	nodeMu.Unlock()

	// 이 지점에서 검증노드에 검증 요청 전송
	/*===================검증요청 하는 코드===================*/
	// 해당 코드(의 통신)를 통해 검증완료된 커미티 정보를 수신함

	var (
		recvPartPubKey []ed25519.PublicKey
		recvPartCommit []cosi.Commitment
		nodeIds        []string
	)

	for _, c := range cands {
		//cands 중 바로 위에서 수신한 데이터의 nodeId정보가 일치하는 노드만 isCommittee에 true
		committeeStruct := CommitteeCandidateInfo{
			Round:       c.Round,
			NodeId:      c.NodeId,
			Seed:        c.Seed,
			Proof:       c.Proof,
			PublicKey:   c.PublicKey,
			Commit:      c.Commit,
			IPAddress:   c.IpAddress,
			Port:        c.Port,
			Channel:     c.Channel,
			IsCommittee: false,
		}
		/* receivedNodeId는 매번 바뀔 수 있음.
		if c.NodeId == receivedNodeId {
			committeeStruct.isCommittee = true
		}*/
		insertNodeInfo(committeeStruct)
	}

	// ② 집계 PK·커밋
	//var nodeIds []string
	for _, c := range cands {
		//mu.Lock()
		recvPartPubKey = append(recvPartPubKey, c.PublicKey)
		recvPartCommit = append(recvPartCommit, c.Commit)
		nodeIds = append(nodeIds, c.NodeId)
		//mu.Unlock()
		log.Printf("recvPartPubKey: %x, recvPartCommit: %x || ",
			c.PublicKey, c.Commit)
	}

	var pubKeys [][]byte
	for _, pk := range recvPartPubKey {
		pubKeys = append(pubKeys, pk)
	}
	log.Println("[recvPartPubKey]:", recvPartPubKey)
	log.Println("[recvPartCommit]:", recvPartCommit)
	log.Println("[pubKeys]:", pubKeys)

	//aggPubKey, aggCommit := aggregatePubKey(recvPartPubKey)
	m.cosigners = cosi.NewCosigners(recvPartPubKey, nil)
	aggPubKey := m.cosigners.AggregatePublicKey()
	aggCommit := m.cosigners.AggregateCommit(recvPartCommit)

	// ③ 브로드캐스트
	m.broadcast(&pb.FinalizedCommittee{
		Round:            round,
		NodeId:           nodeIds,
		AggregatedCommit: aggCommit,
		AggregatedPubKey: aggPubKey,
		PublicKeys:       pubKeys,
	})

	delete(m.committeeCandidates, round) // memory free
	delete(m.processed, round)
}

func (m *meshSrv) gcOldRounds(cur uint64) {
	for r := range m.committeeCandidates {
		if r+gcRoundWindow < cur {
			delete(m.committeeCandidates, r) // GC
		}
	}
}
