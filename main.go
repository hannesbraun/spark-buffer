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
	"strings"
	"time"
)

const Version = "1.1.0"

var debug = false

func main() {
	fmt.Println("spark-buffer", Version)

	hostFlag := flag.String("host", "localhost", "SimSpark server host address")
	portFlag := flag.Uint("port", 3200, "SimSpark server monitor port")
	monitorPortFlag := flag.Uint("monitorPort", 3200, "Monitor output port")
	debugFlag := flag.Bool("debug", false, "Print debug output")
	flag.Parse()
	host := *hostFlag
	port := *portFlag
	monitorOutPort := *monitorPortFlag
	debug = *debugFlag

	toMonitor := make(chan []byte, 1000)
	toServer := make(chan []byte, 1000)
	eofNotifier := make(chan bool)

	go acceptIncomingConnection(int(monitorOutPort), toMonitor, toServer)

	for {
		simsparkAddress := host + ":" + strconv.Itoa(int(port))
		if debug {
			log.Println("Connecting to", simsparkAddress)
		}
		conn, err := net.Dial("tcp", simsparkAddress)
		if err != nil {
			continue
		}
		if debug {
			log.Println("Successfully connected to", simsparkAddress)
		}

		go bufferServerToMonitor(&conn, toMonitor, eofNotifier)
		reconnect := false
		for !reconnect {
			select {
			case msg := <-toServer:
				conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				_, err := conn.Write(msg)
				if err != nil {
					if debug {
						log.Println("Sending message to server:", err)
					}
					_ = conn.Close()
					reconnect = true
				} else {
					if debug {
						log.Println("Command sent")
					}
				}
			case <-eofNotifier:
				_ = conn.Close()
				reconnect = true
			}
		}
	}
}

func acceptIncomingConnection(port int, toMonitor, toServer chan []byte) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port})
	if err != nil {
		log.Fatal(err)
		return
	}

	sendToMonitor := func(conn *net.TCPConn, msg []byte) error {
		_, err = conn.Write(msg)
		if err != nil {
			if debug {
				log.Println("Sending message to monitor:", err)
			}
			return err
		}
		return nil
	}

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			log.Println(err)
			continue
		}

		conn.SetDeadline(time.Time{})

		// Request full state update
		toServer <- encodeSparkMessage("(reqfullstate)")

		go bufferMonitorToServer(conn, toServer)

		// Wait for full state update
		fullStateReceived := false
		for !fullStateReceived {
			msg := <-toMonitor
			if strings.HasPrefix(string(msg[4:]), "((FieldLength") {
				fullStateReceived = true
				err = sendToMonitor(conn, msg)
				if err != nil {
					_ = conn.Close()
					break
				}
			}
		}

		if !fullStateReceived {
			// Send returned error
			continue
		}

		for {
			msg := <-toMonitor
			err = sendToMonitor(conn, msg)
			if err != nil {
				_ = conn.Close()
				break
			}
			time.Sleep(40 * time.Millisecond)
		}
	}
}

func bufferMonitorToServer(conn *net.TCPConn, c chan []byte) {
	msgLenRaw := make([]byte, 4)

	readExact := func(conn *net.TCPConn, buf []byte) error {
		bufTmp := buf

		for len(bufTmp) > 0 {
			r, err := conn.Read(bufTmp)
			if err != nil {
				return err
			}
			bufTmp = bufTmp[r:]
		}

		return nil
	}

	for {
		err := readExact(conn, msgLenRaw)
		//_, err := conn.Read(msgLenRaw)
		if err != nil {
			if debug {
				log.Println("Receiving message header from monitor:", err)
			}
			return
		}

		msgLen := binary.BigEndian.Uint32(msgLenRaw)
		buf := make([]byte, msgLen)
		err = readExact(conn, buf)
		//_, err = conn.Read(buf)
		if err != nil {
			if debug {
				log.Println("Receiving message from monitor:", err)
			}
			return
		}

		c <- append(msgLenRaw, buf...)
	}
}

func bufferServerToMonitor(conn *net.Conn, c chan []byte, eofNotifier chan bool) {
	reader := bufio.NewReader(*conn)
	msgLenRaw := make([]byte, 4)

	for {
		for i := 0; i < 4; i++ {
			lenRaw, err := reader.ReadByte()
			msgLenRaw[i] = lenRaw
			if err != nil {
				if debug {
					log.Println("Receiving message header from server:", err)
				}
				eofNotifier <- true
				return
			}
		}

		msgLen := binary.BigEndian.Uint32(msgLenRaw)
		buf := make([]byte, msgLen)
		_, err := io.ReadFull(reader, buf)
		if err != nil {
			if debug {
				log.Println("Receiving message from server:", err)
			}
			eofNotifier <- true
			return
		}

		c <- append(msgLenRaw, buf...)
	}
}

func encodeSparkMessage(sExp string) []byte {
	msgLen := make([]byte, 4)
	binary.BigEndian.PutUint32(msgLen, uint32(len(sExp)))
	return append(msgLen, []byte(sExp)...)
}
