package main

import (
	"log"
	"net"
	pb "test-sub/proto_interface"
	"time"

	"google.golang.org/grpc"
)

func main() {
	srv := newMeshSrv()

	//테스트용으로 연결된 client에 10초에 한번씩 message broadcast
	go func() {
		for {
			time.Sleep(10 * time.Second)
			srv.broadcast(&pb.CommitteeInfo{
				Origin: "server",
				Data:   []byte(time.Now().Format(time.RFC3339)),
			})
			log.Println("message broadcasted")
		}
	}()

	s := grpc.NewServer()
	pb.RegisterMeshServer(s, srv)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Println("Mesh gRPC server :50051 start")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
