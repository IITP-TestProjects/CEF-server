package main

import (
	"log"
	"net"
	pb "test-server/proto_interface"

	"google.golang.org/grpc"
)

func main() {
	srv := newMeshSrv()

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
