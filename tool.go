package main //차후 directory옮기고, package명 logic, tool등으로 변경

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

const (
	threshold     = 10 // 커미티 선정 시 threshold(노드개수)
	gcRoundWindow = 4  // gcRoundWindow: GC를 위한 라운드 윈도우 크기
)

var connMongo *mongo.Client

// round 기준으로 저장할 데이터 목록
type CommitteeCandidateInfo struct {
	Round       uint64 `bson:"round"`
	NodeId      string `bson:"nodeId"`
	Seed        string `bson:"seed"`
	Proof       []byte `bson:"proof"`
	PublicKey   []byte `bson:"publicKey"`
	Commit      []byte `bson:"commit"`
	IPAddress   string `bson:"ipAddress"`
	Port        string `bson:"port"`
	Channel     string `bson:"channel"`
	IsCommittee bool   `bson:"isCommittee"` // 커미티 선정 여부
	//CreatedAt time.Time `bson:"createdAt"`
	//UpdatedAt time.Time `bson:"updatedAt"`
}

type CommitteeInfo struct {
	ChannelId string   `bson:"channelId"`
	LeaderId  string   `bson:"leaderId"`
	MemberId  []string `bson:"memberId"`
	Timestamp string   `bson:"timestamp"`
}

// mongoDB관련 로직 작성
func activeIndex() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	col := connMongo.Database("node_info").Collection("committee_history")

	// round+nodeId 복합인덱스(조회용): unique index임
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "round", Value: 1},
			{Key: "nodeId", Value: 1},
		},
		Options: options.Index().SetUnique(true).SetName("uniq_round_node"),
	})
	if err != nil {
		return err
	}

	// nodeId 단일 인덱스(삭제용): unique index아님
	_, err = col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "nodeId", Value: 1}},
		Options: options.Index().SetName("idx_nodeId"),
	})

	return err
}

func initMongo() {
	credential := options.Credential{
		Username: "root",
		Password: "root",
	}
	clientOptions := options.Client().
		ApplyURI("mongodb://mongo1:27017, mongodb://mongo2:27018, mongodb://mongo3:27019").
		SetReplicaSet("rs0").
		SetReadPreference(readpref.Primary()).
		SetAuth(credential)

	//("mongodb://mongo:27017").SetAuth(credential)
	var err error
	connMongo, err = mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	//Check the connection
	if err := connMongo.Ping(context.TODO(), nil); err != nil {
		log.Fatalf("Mongo ping error: %v", err)
	}
	fmt.Println("MongoDB Connection Made")

	//연결 결과 출력하는 내용(어떤 DB에 연결되었는지.)
	var result bson.M
	if err := connMongo.Database("admin").
		RunCommand(context.TODO(), bson.D{{Key: "replSetGetStatus", Value: 1}}).
		Decode(&result); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Replica Set Status:", result)

	// 인덱스 활성화
	if err := activeIndex(); err != nil {
		log.Fatalf("Index creation error: %v", err)
	}
	fmt.Println("Index activated")
}

func insertNodeInfo(info CommitteeCandidateInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	col := connMongo.Database("committee_service").Collection("node_history")

	_, err := col.InsertOne(ctx, info)
	return err
}

func insertCommitteeInfo(info *CommitteeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	col := connMongo.Database("committee_service").Collection("committee_history")

	_, err := col.InsertOne(ctx, info)
	return err
}

// delete는 일단 하지 않는것으로(차후에 증거를 위해 데이터를 보존할 필요가 있음)
func deleteNodeInfo(nodeId string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	col := connMongo.Database("node_info").Collection("committee_history")
	res, err := col.DeleteMany(ctx, bson.M{"nodeId": nodeId})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// round, nodeId
func findNodeInfo(round uint64, nodeId string) (*CommitteeCandidateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	col := connMongo.Database("node_info").Collection("committee_history")

	filter := bson.M{
		"round":  round,
		"nodeId": nodeId,
	}

	var result CommitteeCandidateInfo
	err := col.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 못 찾으면 nil 반환
		}
		return nil, err
	}
	return &result, nil
}

/* // commit에 필요한 roundHash 값을 생성
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
} */
