package main //차후 directory옮기고, package명 logic, tool등으로 변경

import (
	"crypto/sha256"
	"encoding/binary"

	"test-server/golang-x-crypto/ed25519"
	"test-server/golang-x-crypto/ed25519/cosi"
)

const (
	threshold     = 10 // 커미티 선정 시 threshold(노드개수)
	gcRoundWindow = 4  // gcRoundWindow: GC를 위한 라운드 윈도우 크기
)

// commit에 필요한 roundHash 값을 생성
func generateRoundHash(round uint64) []byte {
	roundBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(roundBytes, round)

	roundHash := sha256.Sum256(roundBytes)
	return roundHash[:]
}

func aggregatePubKey(pubKeys []ed25519.PublicKey) ([]byte, []byte) {
	//roundHash := generateRoundHash(round)

	cosigners := cosi.NewCosigners(pubKeys, nil)
	commits := []cosi.Commitment{}

	// 안전을 위해서는 각 client에서 직접 commit을 생성하는게 옳다.
	// 테스트이므로 nil로 생성한 commitment를 전송
	for i := 0; i < len(pubKeys); i++ {
		tempCommit, _, err := cosi.Commit(nil)
		if err != nil {
			return nil, nil
		}
		commits = append(commits, tempCommit)
	}

	aggregatePublicKey := cosigners.AggregatePublicKey()
	aggregateCommit := cosigners.AggregateCommit(commits)

	//fmt.Println("Aggregated Public Key: ", aggregatePublicKey)
	//fmt.Println("Aggregated Commit: ", aggregateCommit)

	return aggregatePublicKey, aggregateCommit
}
