package main

import (
	"log"
	pb "test-server/proto_interface"

	"github.com/bford/golang-x-crypto/ed25519/cosi"
)

func (m *meshSrv) appendCandidate(cci *pb.CommitteeCandidateInfo) {
	if m.committeeCandidates == nil {
		m.committeeCandidates = make(map[uint64][]*pb.CommitteeCandidateInfo)
	}
	r := cci.Round
	m.committeeCandidates[r] = append(m.committeeCandidates[r], cci)
}

func (m *meshSrv) tryFinalize(round uint64) {
	cands := m.committeeCandidates[round]
	if len(cands) < threshold {
		return
	}

	// ① metric 기반 위원·프라이머리 선정 (별도 RPC 호출)
	// MCNL 협의 필요한 부분
	// selectPrimaryAndCommittee(cands) ...

	// ② 집계 PK·커밋
	var nodeIds []string
	for _, c := range cands {
		mu.Lock()
		recvPartPubKey = append(recvPartPubKey, c.PublicKey)
		recvPartCommit = append(recvPartCommit, c.Commit)
		nodeIds = append(nodeIds, c.NodeId)
		mu.Unlock()
		log.Printf("recvPartPubKey: %x, recvPartCommit: %x",
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
	cosigners := cosi.NewCosigners(recvPartPubKey, nil)
	aggPubKey := cosigners.AggregatePublicKey()
	aggCommit := cosigners.AggregateCommit(recvPartCommit)

	//log.Println("\naggPubKey:", aggPubKey, "\naggCommit", aggCommit)

	// ③ 브로드캐스트
	m.broadcast(&pb.FinalizedCommittee{
		Round:            round,
		NodeId:           nodeIds,
		AggregatedCommit: aggCommit,
		AggregatedPubKey: aggPubKey,
		PublicKeys:       pubKeys,
	})

	delete(m.committeeCandidates, round) // memory free
	aggPubKey = nil
	aggCommit = nil
	recvPartPubKey = nil
}

func (m *meshSrv) gcOldRounds(cur uint64) {
	for r := range m.committeeCandidates {
		if r+gcRoundWindow < cur {
			delete(m.committeeCandidates, r) // GC
		}
	}
}
