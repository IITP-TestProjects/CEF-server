package main

import (
	"context"
	"log"
	pv "test-server/proto_verify"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// 현재 구현 상 JoinNetwork사라지며 subscribe 필요없어짐

func initVerifyClient(verifyNodeAddress string) pv.CommitteeServiceClient {
	conn, err := grpc.NewClient(verifyNodeAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}

	conn.Connect()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 상태 루프: READY가 되면 성공으로 판단
	for {
		st := conn.GetState()
		if st == connectivity.Ready {
			log.Printf("verify client connected: %s", verifyNodeAddress)
			break
		}
		// 타임아웃 혹은 상태 변화 대기 실패 시 에러 처리
		if !conn.WaitForStateChange(ctx, st) {
			_ = conn.Close()
			log.Fatalf("verify client not ready before timeout (last state=%s, addr=%q)", st.String(), verifyNodeAddress)
			return nil
		}
		// Shutdown으로 떨어지면 즉시 실패
		if conn.GetState() == connectivity.Shutdown {
			_ = conn.Close()
			log.Fatalf("verify client entered Shutdown state (addr=%q)", verifyNodeAddress)
			return nil
		}
	}

	verifyClient := pv.NewCommitteeServiceClient(conn)
	if verifyClient == nil {
		log.Println("verifyClient is nil")
		return nil
	} else {
		log.Println("verifyClient initialized (ready)")
	}

	return verifyClient
}
