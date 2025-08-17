package main

import (
	"log"
	pb "test-server/proto_interface"
	pv "test-server/proto_verify"
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

	if m.verifyCommCh == nil {
		m.verifyCommCh = make(map[uint64]chan *pv.CommitteeInfo)
	}
}

func insertCandidateNodes(local []*pb.CommitteeCandidateInfo) {
	//차후 아래 내용 subscribe 코드에 통합필요
	for _, c := range local {
		//cands 중 바로 위에서 수신한 데이터의 nodeId정보가 일치하는 노드만 isCommittee에 true
		committeeStruct := &CommitteeCandidateInfo{
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
		insertNodeInfo(*committeeStruct)
	}
}

// 후보자정보 등록 m.mu.Lock()으로 들어온 함수
func (m *meshSrv) appendCandidate(cci *pb.CommitteeCandidateInfo) bool {
	r := cci.Round

	m.mu.Lock()
	m.initMaps()

	if m.processed[cci.Round] {
		m.mu.Unlock()
		return false
	}
	m.committeeCandidates[r] = append(m.committeeCandidates[r], cci)

	if _, ok := m.timers[r]; !ok {
		m.timers[r] = time.AfterFunc(maxWait, func() {
			log.Println("timeout! begin to make commmittee!")
			m.tryFinalize(r)
		})
	}

	reached := len(m.committeeCandidates[r]) >= threshold
	if reached {
		if t := m.timers[r]; t != nil {
			t.Stop()
		}
	}
	m.mu.Unlock()

	if reached {
		go m.tryFinalize(r)
	}
	return true
}

// m.mu.Lock()으로 들어온 함수
func (m *meshSrv) tryFinalize(round uint64) {
	m.mu.Lock()
	if m.processed[round] {
		m.mu.Unlock()
		return
	}
	m.processed[round] = true

	if t, ok := m.timers[round]; ok && t != nil {
		t.Stop()
		delete(m.timers, round)
	}

	cands := m.committeeCandidates[round]
	local := make([]*pb.CommitteeCandidateInfo, len(cands))
	copy(local, cands)

	delete(m.committeeCandidates, round) // memory free
	m.mu.Unlock()

	/* roundMu.Lock()
	processingRound = round
	roundMu.Unlock() */

	insertCandidateNodes(local) // 후보자 정보 DB에 저장

	// 이 지점에서 검증노드에 검증 요청 전송
	// verfyClient로 subscribe한 곳에서 수신함
	/* m.verifyCli.RequestCommittee(context.Background(),
	&pv.CommitteeRequest{
		//내부 데이터는 협의 완려되는대로 구성
	}) */

	// ① metric 기반 위원·프라이머리 선정 (별도 RPC 호출)
	// MCNL 협의 필요한 부분
	// selectPrimaryAndCommittee(cands) ...

	var (
		recvPartPubKey []ed25519.PublicKey
		recvPartCommit []cosi.Commitment
		nodeIds        []string //앞으로 노드 아이디를 verify node로부터 받아옴
	)

	// ② 집계 PK·커밋
	//var nodeIds []string
	for _, c := range local {
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
	m.mu.Lock()
	m.cosigners = cosi.NewCosigners(recvPartPubKey, nil)
	m.mu.Unlock()
	aggPubKey := m.cosigners.AggregatePublicKey()
	aggCommit := m.cosigners.AggregateCommit(recvPartCommit)

	nodeMu.Lock()
	nodeNumber = len(nodeIds)
	nodeMu.Unlock()

	/* m.mu.Lock()
	ch, ok := m.verifyCommCh[processingRound]
	if !ok {
		ch = make(chan *pv.CommitteeInfo, 1)
		m.verifyCommCh[processingRound] = ch
	}
	m.mu.Unlock()

	var vmsg *pv.CommitteeInfo
	select {
	case vmsg = <-ch:
		// vmsg 내용으로 cands 표식/선정 등 반영
		// ex) vmsg.MemberIds 를 기반으로 cands 안 isCommittee 설정
	case <-time.After(5 * time.Second):
		// 응답이 늦으면 없이 진행 (정책에 따라 재시도/로그)
		log.Println("verify response timeout: proceed without it")
	} */

	// ③ 브로드캐스트
	m.broadcast(&pb.FinalizedCommittee{
		Round: round,
		//NodeId:           vmsg.MemberIds,
		//LeaderNodeId:     vmsg.LeaderMemberId,
		NodeId:           nodeIds,
		AggregatedCommit: aggCommit,
		AggregatedPubKey: aggPubKey,
		PublicKeys:       pubKeys,
	})

	m.mu.Lock()
	delete(m.committeeCandidates, round) // memory free
	delete(m.verifyCommCh, round)
	delete(m.processed, round)
	m.mu.Unlock()
}

func (m *meshSrv) gcOldRounds(cur uint64) {
	for r := range m.committeeCandidates {
		if r+gcRoundWindow < cur {
			delete(m.committeeCandidates, r) // GC
		}
	}
}
