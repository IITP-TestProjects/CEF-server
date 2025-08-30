package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	pb "test-server/proto_interface"

	"google.golang.org/grpc"
)

func main() {
	verifyServerAddress := flag.String(
		"verifynode",
		"0",
		"CEF requires the address of the verification node (e.g., -verifynode=localhost:50051)",
	)
	flag.Parse()
	if *verifyServerAddress == "0" {
		fmt.Println("Please provide the address of the verification node (e.g., -verifynode=localhost:50051)")
		return
	}

	log.Println("verif server:", *verifyServerAddress)
	//verify server(인천대)연결 클라이언트 인스턴스 생성
	verifyClient := initVerifyClient(*verifyServerAddress)
	srv := newMeshSrv(verifyClient)
	initMongo()

	//CEF 서버 인스턴스 생성
	s := grpc.NewServer()
	pb.RegisterMeshServer(s, srv)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Println("CEF gRPC server :50051 start")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
