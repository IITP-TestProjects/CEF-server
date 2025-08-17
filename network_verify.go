package main

import (
	"context"
	"io"
	"log"
	pv "test-server/proto_verify"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CommitteeService.Join을 호출해 서버 스트림을 열고,
// 별도 goroutine에서 Recv()로 메시지를 계속 수신한다(subscribe).
func (m *meshSrv) subscribe(c pv.CommitteeServiceClient) error {
	//Join rpc 관련해서 답변 대기중............
	stream, err := c.Join(context.Background(),
		&pv.JoinRequest{
			NodeId:    "CEF-Server",
			Addr:      "myIP(CEF)",
			Port:      "50051",
			Publickey: "test-public-key",
		})
	if err != nil {
		return err
	}

	go func() {
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				log.Println("stream closed")
				return
			}
			if err != nil {
				return
			}

			// 검증노드로부터 수신한 커미티정보 DB에 저장
			err = insertCommitteeInfo(&CommitteeInfo{
				ChannelId: msg.ChannelId,
				LeaderId:  msg.LeaderMemberId,
				MemberId:  msg.MemberIds,
				Timestamp: msg.Timestamp,
			})
			if err != nil {
				log.Println("Error inserting committee info:", err)
			}

			nodeMu.Lock()
			nodeNumber = len(msg.MemberIds)
			nodeMu.Unlock()

			// 채널로 신호 전송
			m.mu.Lock()
			roundMu.RLock()
			ch, ok := m.verifyCommCh[processingRound]
			if !ok {
				ch = make(chan *pv.CommitteeInfo, 1) // 버퍼 1: tryFinalize가 아직 대기 안 해도 누락 방지
				m.verifyCommCh[processingRound] = ch
			}
			roundMu.RUnlock()
			m.mu.Unlock()

			select {
			case ch <- msg:
			default:
				select {
				case <-ch:
				default:
				}
				ch <- msg
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

	return verifyClient
}
