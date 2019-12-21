package main

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Message structure for components passed over TCP
type Message struct {
	IsName  bool
	Name    string
	Content string
	Ip      string
	Id      int
}

var name string
var counter int
var my_ip string
var hostnames []string

var vm_names sync.Map
var vm_enc sync.Map   // for sending
var received sync.Map // map[string]bool

func main() {
	hostnames = []string{
		"sp19-cs425-g31-01.cs.illinois.edu",
		"sp19-cs425-g31-02.cs.illinois.edu",
		"sp19-cs425-g31-03.cs.illinois.edu",
		"sp19-cs425-g31-04.cs.illinois.edu",
		"sp19-cs425-g31-05.cs.illinois.edu",
		"sp19-cs425-g31-06.cs.illinois.edu",
		"sp19-cs425-g31-07.cs.illinois.edu",
		"sp19-cs425-g31-08.cs.illinois.edu",
		"sp19-cs425-g31-09.cs.illinois.edu",
		"sp19-cs425-g31-10.cs.illinois.edu",
	}

	// read in command-line arguments
	name = os.Args[1]
	port := os.Args[2]
	n := os.Args[3]

	// get number of machines in chat
	n_machines_int, err := strconv.Atoi(n)
	if err != nil {
		panic(err)
	}

	// listen on given port
	var port_str string = ":" + port
	ln, err := net.Listen("tcp", port_str)
	if err != nil {
		panic(err)
	}

	// spawn goroutine to receive chat aliases on startup
	go receiveNames(hostnames, port)

	// server accepts a connection for each client
	num_machines := 0
	for num_machines < n_machines_int {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}

		// remove port from remote IP address
		remote_ip := strings.Split(conn.RemoteAddr().String(), ":")[0]

		// create a serializer for the connection and save it
		encoder := gob.NewEncoder(conn)
		vm_enc.Store(remote_ip, encoder)

		// tracks whether users have disconnected
		go trackConnection(conn, encoder)

		num_machines++
	}

	// announce chat ready and accept typed input
	fmt.Println("READY")
	readInput()
}

// receiveNames receives chat names from every chat participant
// stores chat names to identify users in chat
func receiveNames(hostnames []string, port string) {
	for _, machine := range hostnames {
		go func(machine string) {
			machine_addr := machine + ":" + port
			vm_name, conn, dec := establishConnection(machine_addr)
			ip_addr := strings.Split(conn.RemoteAddr().String(), ":")[0]
			my_ip = strings.Split(conn.LocalAddr().String(), ":")[0]

			if vm_name != "" {
				vm_names.Store(ip_addr, vm_name)
				receiveMessages(dec)
			}
		}(machine)
	}
}

// establishConnection retrieves chat names of users
// decodes information from initial TCP receive on connection
func establishConnection(remote string) (string, net.Conn, *gob.Decoder) {
	for {
		conn, err := net.Dial("tcp", remote)

		if err == nil {
			dec := gob.NewDecoder(conn)
			msg_data := Message{}
			err := dec.Decode(&msg_data)
			if err == nil {
				return msg_data.Name, conn, dec
			}
		}
	}
}

// receiveMessages accepts incoming message packets
// implements R-multicast to display and forward messages
func receiveMessages(dec *gob.Decoder) {
	// continually receive messages
	for {
		// build struct to place decoded message packet
		msg_data := Message{}
		err := dec.Decode(&msg_data)

		// on a successful deserialization
		if err == nil {
			// if data packet, not chat alias message
			if !msg_data.IsName {
				// generate unique identifier to track if message already received
				unique_id := msg_data.Ip + "/" + strconv.Itoa(msg_data.Id)
				// atomically check and update message receive state
				if _, loaded := received.LoadOrStore(unique_id, true); !loaded {
					// if not sending to self
					if my_ip != msg_data.Ip {
						// iterate over each chat user and forward the message
						go func() {
							vm_enc.Range(func(key interface{}, value interface{}) bool {
								_, ok := key.(string)
								if ok {
									encoder, ok := value.(*gob.Encoder)
									if ok {
										for {
											err := encoder.Encode(msg_data)
											if err == nil {
												return true
											}
										}
									}
								}
								return false
							})
						}()
						// print the message, prefixed with chat alias of original sender
						fmt.Printf("%s: %s", msg_data.Name, msg_data.Content)
					}
				}
			}
			// remote user has disconnected
		} else if err == io.EOF {
			return
		}
	}
}

// trackConnection keeps track of open TCP connections
// when anther user exits (e.g., via Ctrl-C), handles error and logs in chat
func trackConnection(c net.Conn, encoder *gob.Encoder) {
	// remove port from IP address of self
	parts := strings.Split(c.LocalAddr().String(), ":")

	// construct a message packet to send chat alias
	// Content and Id fields irrelevant
	message_data := Message{IsName: true, Name: name, Content: "", Ip: parts[0], Id: 0}

	// try until serialization successful
	for {
		err := encoder.Encode(message_data)
		// if TCP connection has error (failed)
		if err != nil {
			// parse out IP address of user connected to
			parts := strings.Split(c.RemoteAddr().String(), ":")
			// retrieve chat alias of user and display user has left
			value, ok := vm_names.Load(parts[0])
			if ok {
				fmt.Println(value, "has left")
			}
			return
		}
	}
}

// readInput ingests typed messages from the console
// broadcasts message to all connected chat users
func readInput() {
	// keep track of how many messages user is sending
	counter = 0
	// read from standard in (console)
	reader := bufio.NewReader(os.Stdin)
	for {
		// check when user presses return
		text, err := reader.ReadString('\n')
		if err == nil {
			// increment message counter
			counter++
			// send message to every other user
			vm_enc.Range(func(key interface{}, value interface{}) bool {
				_, ok := key.(string)
				if ok {
					encoder, ok := value.(*gob.Encoder)
					if ok {
						// build message packet and serialize for gob sending
						message_data := Message{IsName: false, Name: name, Content: text, Ip: my_ip, Id: counter}
						for {
							err := encoder.Encode(message_data)
							if err == nil {
								return true
							}
						}
					}
				}
				return false
			})
		}
	}
}
