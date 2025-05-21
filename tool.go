package main //차후 directory옮기고, package명 logic, tool등으로 변경

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"

	"github.com/bford/golang-x-crypto/ed25519"
	"github.com/bford/golang-x-crypto/ed25519/cosi"
)

const (
	threshold     = 4 // 커미티 선정 시 threshold
	gcRoundWindow = 4 // gcRoundWindow: GC를 위한 라운드 윈도우 크기
)

var recvPartPubKey []ed25519.PublicKey

// commit에 필요한 roundHash 값을 생성
func generateRoundHash(round uint64) []byte {
	roundBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(roundBytes, round)

	roundHash := sha256.Sum256(roundBytes)
	return roundHash[:]
}

func aggregatePubKey(pubKeys []ed25519.PublicKey, round uint64) ([]byte, []byte) {
	roundHash := generateRoundHash(round)

	cosigners := cosi.NewCosigners(pubKeys, nil)
	commits := []cosi.Commitment{}

	// 차후 클라이언트에서 압축된 서명을 검증할 때도 roundHash값이 필요하다.
	for i := 0; i < len(pubKeys); i++ {
		tempCommit, _, _ := cosi.Commit(bytes.NewReader(roundHash[:]))
		commits = append(commits, tempCommit)
	}

	aggregatePublicKey := cosigners.AggregatePublicKey()
	aggregateCommit := cosigners.AggregateCommit(commits)

	//fmt.Println("Aggregated Public Key: ", aggregatePublicKey)
	//fmt.Println("Aggregated Commit: ", aggregateCommit)

	return aggregateCommit, aggregatePublicKey
}
