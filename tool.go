package main //차후 directory옮기고, package명 logic, vrf등으로 변경

import (
	"github.com/bford/golang-x-crypto/ed25519"
	"github.com/bford/golang-x-crypto/ed25519/cosi"
)

//aggregatePubKey에 사용할 struct구성 필요.

func aggregatePubKey(pubKeys []ed25519.PublicKey) ([]byte, []byte) {
	cosigners := cosi.NewCosigners(pubKeys, nil)
	commits := []cosi.Commitment{}

	for i := 0; i < len(pubKeys); i++ {
		tempCommit, _, _ := cosi.Commit(nil)
		commits = append(commits, tempCommit)
	}

	aggregatePublicKey := cosigners.AggregatePublicKey()
	aggregateCommit := cosigners.AggregateCommit(commits)

	//fmt.Println("Aggregated Public Key: ", aggregatePublicKey)
	//fmt.Println("Aggregated Commit: ", aggregateCommit)

	return aggregateCommit, aggregatePublicKey
}
