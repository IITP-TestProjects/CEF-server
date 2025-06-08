package main

import (
	"log"
	pb "test-server/proto_interface"
	"time"

	"github.com/bford/golang-x-crypto/ed25519"
	"github.com/bford/golang-x-crypto/ed25519/cosi"
)

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

	// 현재는 threshold에 의해 커미티 개수가 결정되지만,
	// 추후에는 동적인 방식에 의해 candidate를 수집 -> 추가됐으므로 해당방식 패기
	/* if len(cands) < threshold {
		return
	} */

	// ① metric 기반 위원·프라이머리 선정 (별도 RPC 호출)
	// MCNL 협의 필요한 부분
	// selectPrimaryAndCommittee(cands) ...
	// 커미티 정보를 받았을 때 개수를 전역변수에 저장.
	nodeMu.Lock()
	nodeNumber = len(cands)
	//nodeNumber = threshold // 현재는 threshold로 고정
	nodeMu.Unlock()

	var (
		recvPartPubKey []ed25519.PublicKey
		recvPartCommit []cosi.Commitment
		nodeIds        []string
	)

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
