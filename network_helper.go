package main

import pb "test-server/proto_interface"

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

	// ① metric 기반 위원·프라이머리 선정 (별도 모듈 호출)
	// selectPrimaryAndCommittee(cands) ...

	// ② 집계 PK·커밋
	var nodeIds []string
	for _, c := range cands {
		recvPartPubKey = append(recvPartPubKey, c.PublicKey)
		nodeIds = append(nodeIds, c.NodeId)
	}
	aggPubKey, aggCommit := aggregatePubKey(recvPartPubKey, round)

	// ③ 브로드캐스트
	m.broadcast(&pb.FinalizedCommittee{
		Round:            round,
		NodeId:           nodeIds,
		AggregatedCommit: aggCommit,
		AggregatedPubKey: aggPubKey,
	})

	delete(m.committeeCandidates, round) // memory free
}

func (m *meshSrv) gcOldRounds(cur uint64) {
	for r := range m.committeeCandidates {
		if r+gcRoundWindow < cur {
			delete(m.committeeCandidates, r) // GC
		}
	}
}
