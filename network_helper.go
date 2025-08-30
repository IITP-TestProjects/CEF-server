package main

import (
	"context"
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
			m.finalizeCommitteeSelection(r)
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
		go m.finalizeCommitteeSelection(r)
	}
	return true
}

func (m *meshSrv) buildVerifyRequestData(cands []*pb.CommitteeCandidateInfo) []*pv.CommitteeCandidates {
	candidateGroup := make([]*pv.CommitteeCandidates, 0)
	for _, data := range cands {
		CandidateNodeData := &pv.CommitteeCandidates{
			NodeId:    data.NodeId,
			Addr:      data.IpAddress,
			Port:      data.Port,
			Publickey: data.PublicKey,
			Seed:      data.Seed,
			Proof:     data.Proof,
		}
		candidateGroup = append(candidateGroup, CandidateNodeData)
	}
	return candidateGroup
}

func (m *meshSrv) aggregateKeyAndCommits(local *[]*pb.CommitteeCandidateInfo) ([][]byte, ed25519.PublicKey, cosi.Commitment) {
	var (
		recvPartPubKey []ed25519.PublicKey
		recvPartCommit []cosi.Commitment
		//pubKeys        [][]byte
	)

	for _, c := range *local {
		recvPartPubKey = append(recvPartPubKey, c.PublicKey)
		recvPartCommit = append(recvPartCommit, c.Commit)
		//nodeIds = append(nodeIds, c.NodeId)
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

	m.mu.Lock()
	m.cosigners = cosi.NewCosigners(recvPartPubKey, nil)
	m.mu.Unlock()
	aggPubKey := m.cosigners.AggregatePublicKey()
	aggCommit := m.cosigners.AggregateCommit(recvPartCommit)

	return pubKeys, aggPubKey, aggCommit
}

func deleteExceptCommittee(local *[]*pb.CommitteeCandidateInfo, vmsg *pv.CommitteeInfo) {
	validNodeIds := make(map[string]struct{})
	for _, id := range vmsg.MemberIds {
		validNodeIds[id] = struct{}{}
	}

	filteredLocal := make([]*pb.CommitteeCandidateInfo, 0, len(*local))

	for _, candidate := range *local {
		if _, ok := validNodeIds[candidate.NodeId]; ok {
			filteredLocal = append(filteredLocal, candidate)
		}
	}
	*local = filteredLocal
}

func (m *meshSrv) finalizeCommitteeSelection(round uint64) {
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

	insertCandidateNodes(local)                       // 후보자 정보 DB에 저장
	candidateGroup := m.buildVerifyRequestData(local) // 검증 요청에 필요한 데이터 빌드

	// ① 커미티 및 리더노드 선정(검증노드에 커미티 생성 요청)
	log.Println("Requesting committee selection to verify node...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vmsg, err := m.verifyCli.RequestCommittee(ctx,
		&pv.CommitteeRequest{
			Candidates: candidateGroup,
			Channel:    local[0].Channel, //어쨋든 candidate node들은 동일한 채널에 대해 요청함
		})
	if err != nil {
		log.Println("verifyCli.RequestCommittee error:", err)
	}

	if vmsg == nil {
		log.Println("nothing vmsg")
	}

	//local 중 vmsg에서 선택되지 않은 데이터들은 모두 삭제처리
	deleteExceptCommittee(&local, vmsg)

	//압축에 필요한 데이터 생성.
	pubKeys, aggPubKey, aggCommit := m.aggregateKeyAndCommits(&local)

	nodeMu.Lock()
	nodeNumber = len(vmsg.MemberIds) //이후 RequestAggregatedCommit에 필요한 데이터
	nodeMu.Unlock()

	// ③ 블록체인 노드 측으로 브로드캐스트
	m.broadcast(&pb.FinalizedCommittee{
		Round:            round,
		Channel:          local[0].Channel,
		NodeId:           vmsg.MemberIds,
		LeaderNodeId:     vmsg.LeaderMemberId,
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
