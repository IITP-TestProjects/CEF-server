package main

import (
	"log"
	"net"
	pb "test-server/proto_interface"

	"google.golang.org/grpc"
)

const verifyServerAddress = "localhost:50052"

func main() {
	//verify server(인천대)연결 클라이언트 인스턴스 생성 및 subscribe
	//verifyClient := initVerifyClient()
	srv := newMeshSrv( /* verifyClient */ )
	//srv.subscribe(verifyClient) //verify Node에 subscribe
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
