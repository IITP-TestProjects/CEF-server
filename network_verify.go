package main

import (
	"context"
	"log"
	pv "test-server/proto_verify"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CommitteeService.Join을 호출해 서버 스트림을 열고,
// 별도 goroutine에서 Recv()로 메시지를 계속 수신한다(subscribe).
func subscribe(c pv.CommitteeServiceClient) error {
	stream, err := c.Join(context.Background(),
		&pv.JoinRequest{
			NodeId:    "test-node",
			Addr:      "111.111.111",
			Port:      "50051",
			Publickey: "test-public-key",
		})
	if err != nil {
		return err
	}

	go func() {
		for {
			_, err := stream.Recv()
			if err != nil {
				return
			}
		}
	}()

	return nil
}

func initVerifyClient() pv.CommitteeServiceClient {
	conn, err := grpc.NewClient(verifyServerAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}

	verifyClient := pv.NewCommitteeServiceClient(conn)
	subscribe(verifyClient)

	return verifyClient
}
