package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"time"
)

const Version = "1.0.0"

func main() {
	fmt.Println("spark-cache", Version)

	hostFlag := flag.String("host", "localhost", "SimSpark server host address")
	portFlag := flag.Uint("port", 3200, "SimSpark server monitor port")
	monitorPortFlag := flag.Uint("monitorPort", 3200, "Monitor output port")
	flag.Parse()
	host := *hostFlag
	port := *portFlag
	monitorOutPort := *monitorPortFlag

	toMonitor := make(chan []byte, 1000)
	toServer := make(chan []byte, 1000)

	go acceptIncomingConnection(int(monitorOutPort), toMonitor, toServer)

	for {
		simsparkAddress := host + ":" + strconv.Itoa(int(port))
		conn, err := net.Dial("tcp", simsparkAddress)
		if err != nil {
			continue
		}

		go cacheServerToMonitor(&conn, toMonitor)
		for {
			msg := <-toServer
			_, err := conn.Write(msg)
			if err != nil {
				break
			}
		}
	}
}

func acceptIncomingConnection(port int, toMonitor, toServer chan []byte) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port})
	if err != nil {
		return
	}

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			log.Println(err)
			continue
		}

		conn.SetDeadline(time.Time{})

		go cacheMonitorToServer(conn, toServer)
		go func() {
			for {
				msg := <-toMonitor
				_, err := conn.Write(msg)
				if err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
}

func cacheMonitorToServer(conn *net.TCPConn, c chan []byte) {
	msgLenRaw := make([]byte, 4)

	for {
		_, err := conn.Read(msgLenRaw)
		if err != nil {
			log.Println(err)
			return
		}

		msgLen := binary.BigEndian.Uint32(msgLenRaw)
		buf := make([]byte, msgLen)
		_, err = conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}

		c <- append(msgLenRaw, buf...)
	}
}

func cacheServerToMonitor(conn *net.Conn, c chan []byte) {
	reader := bufio.NewReader(*conn)
	msgLenRaw := make([]byte, 4)

	for {
		for i := 0; i < 4; i++ {
			lenRaw, err := reader.ReadByte()
			msgLenRaw[i] = lenRaw
			if err != nil {
				log.Println(err)
				return
			}
		}

		msgLen := binary.BigEndian.Uint32(msgLenRaw)
		buf := make([]byte, msgLen)
		_, err := io.ReadFull(reader, buf)
		if err != nil {
			log.Println(err)
			return
		}

		c <- append(msgLenRaw, buf...)
	}
}
