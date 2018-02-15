package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"grepserver"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"utils"
)

const GATEWAY = "172.31.26.66/20"

const MIN_GROUP_SIZE = 4

const MAX_TIME = time.Millisecond * 2500

var currHost string

var partofGroup int

var mutex = &sync.Mutex{}

var timers [3]*time.Timer

var resetTimerFlags [3]int

var membershipGroup = make([]member, 0)

type message struct {
	Host      string
	Status    string
	TimeStamp string
}

type member struct {
	Host      string
	TimeStamp string
}

//For logging
var logfile *os.File
var errlog *log.Logger
var infolog *log.Logger
var emptylog *log.Logger

func main() {
	initDatas()

	go listenMessages()
	go listenGateway()
	go sendSyn()
	go checkAck(1)
	go checkAck(2)
	go checkAck(3)
	go grepserver.StartGrepServer()

	takeUserInput()
}

func takeUserInput() {

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("1 Print membership list")
		fmt.Println("2 Print self ID")
		fmt.Println("3 Join group")
		fmt.Println("4 Leave group")
		fmt.Println("5 Grep node logs\n")
		fmt.Println("Enter option: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSuffix(input, "\n")
		switch input {
		case "1":
			for _, element := range membershipGroup {
				fmt.Println(element)
			}
		case "2":
			fmt.Println(currHost)
		case "3":
			if currHost != GATEWAY && partofGroup == 0 {
				fmt.Println("Joining group")
				gatewayConnect()
				partofGroup = 1
			} else {
				fmt.Println("I am Master or I am already connected")
			}
		case "4":
			if partofGroup == 1 {
				fmt.Println("Leaving group")
				exitGroup()
				os.Exit(0)

			} else {
				fmt.Println("You are currently not connected to a group or You are master")
			}
		case "5":
			grepClient(reader)
		default:
			fmt.Println("Invalid command")
		}
		fmt.Println("\n\n")
	}
}

/*
 * Run grep on the servers
 */
func grepClient(reader *bufio.Reader) {

	fmt.Println("Usage: -options keywordToSearch")
	fmt.Println("-options: available in linux grep command")
	fmt.Println("Enter: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSuffix(input, "\n")
	serverInput := strings.Split(input, " ")
	c := make(chan string)
	// Send data to every server in membershipList
	for _, element := range membershipGroup {
		localip, _, _ := net.ParseCIDR(element.Host)
		go utils.SendToServer(localip.String()+":"+grepserver.PORT, serverInput, c)
	}

	// Print results from server
	for i, _ := range membershipGroup {
		serverResult := <-c
		fmt.Println(serverResult)
		fmt.Printf("END----------%d\n", i)
	}
}

func listenMessages() {
	Addr, err := net.ResolveUDPAddr("udp", ":10000")
	if err != nil {
		fmt.Println("listenmessages:Not able to resolve udp")
		errlog.Println(err)
	}
	Conn, err := net.ListenUDP("udp", Addr)
	if err != nil {
		fmt.Println("listenmessages:Not able to resolve listen to UDP")
		errlog.Println(err)
	}
	defer Conn.Close()

	buf := make([]byte, 1024)

	for {
		pkt := message{}
		n, _, err := Conn.ReadFromUDP(buf)
		err = gob.NewDecoder(bytes.NewReader(buf[:n])).Decode(&pkt)
		if err != nil {
			fmt.Println("listenmessages:Not able to read from Conn")
			errlog.Println(err)
		}
		switch pkt.Status {
		case "Join":
			node := member{pkt.Host, time.Now().Format(time.RFC850)}
			if checkTimeStamp(node) == 0 {
				mutex.Lock()
				resetCorrespondingTimers()
				membershipGroup = append(membershipGroup, node)
				mutex.Unlock()
			}
			broadcastGroup()
		case "SYN":
			respondAck(pkt.Host)
		case "ACK":
			if pkt.Host == membershipGroup[(getIx()+1)%len(membershipGroup)].Host {
				timers[0].Reset(MAX_TIME)
			} else if pkt.Host == membershipGroup[(getIx()+2)%len(membershipGroup)].Host {
				timers[1].Reset(MAX_TIME)
			} else if pkt.Host == membershipGroup[(getIx()+3)%len(membershipGroup)].Host {
				timers[2].Reset(MAX_TIME)
			}
			infolog.Println("ACK response: " + time.Now())
		case "Failed":
			mutex.Lock()
			resetCorrespondingTimers()
			spreadGroup(pkt)
			mutex.Unlock()
		case "Leave":
			mutex.Lock()
			resetCorrespondingTimers()
			spreadGroup(pkt)
			mutex.Unlock()

		}
	}
}

func listenGateway() {
	Addr, err := net.ResolveUDPAddr("udp", ":10001")
	if err != nil {
		fmt.Println("listen gateway:Not able to resolve udp")
		errlog.Println(err)
	}

	Conn, err := net.ListenUDP("udp", Addr)
	if err != nil {
		fmt.Println("listen gateway:Not able to resolve udp")
		errlog.Println(err)
	}
	defer Conn.Close()

	buf := make([]byte, 1024)

	for {
		list := make([]member, 0)
		n, _, err := Conn.ReadFromUDP(buf)
		err = gob.NewDecoder(bytes.NewReader(buf[:n])).Decode(&list)
		if err != nil {
			fmt.Println("listen gateway:Not able to resolve udp")
			errlog.Println(err)
		}

		//restart timers if membershipList is updated
		mutex.Lock()
		resetCorrespondingTimers()
		membershipGroup = list
		mutex.Unlock()
		
		var info = "New VM joined the group: \n\t["
		var N = len(list) - 1
		for i, host := range list {
			info += "(" + host.Host + " | " + host.TimeStamp + ")"
			if i != N {
				info += ", \n\t"
			} else {
				info += "]"
			}
		}
		infolog.Println(info)
	}
}

func checkAck(relativeIx int) {

	for len(membershipGroup) < MIN_GROUP_SIZE {
		time.Sleep(100 * time.Millisecond)
	}

	host := membershipGroup[(getIx()+relativeIx)%len(membershipGroup)].Host
	fmt.Print("Checking ")
	fmt.Print(relativeIx)
	fmt.Print(": ")
	fmt.Println(host)

	timers[relativeIx-1] = time.NewTimer(MAX_TIME)
	<-timers[relativeIx-1].C

	mutex.Lock()
	if len(membershipGroup) >= MIN_GROUP_SIZE && getRelativeIx(host) == relativeIx && resetTimerFlags[relativeIx-1] != 1 {
		msg := message{membershipGroup[(getIx()+relativeIx)%len(membershipGroup)].Host, "Failed", time.Now().Format(time.RFC850)}
		fmt.Print("Failure detected: ")
		fmt.Println(msg.Host)
		spreadGroup(msg)

	}
	//If a failure is detected for one timer, reset the others as well.
	if resetTimerFlags[relativeIx-1] == 0 {
		fmt.Print("Force stopping other timers")
		fmt.Println(relativeIx)
		for i := 1; i < 3; i++ {
			resetTimerFlags[i] = 1
			timers[i].Reset(0)
		}
	} else {
		fmt.Println(relativeIx)
		resetTimerFlags[relativeIx-1] = 0
	}

	mutex.Unlock()
	go checkAck(relativeIx)

}

func initMG() {
	node := member{currHost, time.Now().Format(time.RFC850)}
	membershipGroup = append(membershipGroup, node)
}

func initDatas() {

	currHost = getIP()
	initMG()
	timers[0] = time.NewTimer(MAX_TIME)
	timers[1] = time.NewTimer(MAX_TIME)
	timers[2] = time.NewTimer(MAX_TIME)
	timers[0].Stop()
	timers[1].Stop()
	timers[2].Stop()

	absPath, _ := filepath.Abs(utils.LOG_FILE_GREP)
	fmt.Println("Log File path: " + absPath)

	logfile_exists := 1
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		logfile_exists = 0
	}

	logfile, _ := os.OpenFile(absPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	errlog = log.New(logfile, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	infolog = log.New(logfile, "INFO: ", log.Ldate|log.Ltime)
	emptylog = log.New(logfile, "\n----------------------------------------------------------------------------------------\n", log.Ldate|log.Ltime)

	if logfile_exists == 1 {
		emptylog.Println("")
	}

}

func updateMG(Ix int, msg message) int {
	localTime, _ := time.Parse(time.RFC850, membershipGroup[Ix].TimeStamp)
	givenTime, _ := time.Parse(time.RFC850, msg.TimeStamp)

	if givenTime.After(localTime) {
		membershipGroup = append(membershipGroup[:Ix], membershipGroup[Ix+1:]...)
		return 1
	} else {
		return 0
	}
}

func resetCorrespondingTimers() {
	resetTimerFlags[0] = 1
	resetTimerFlags[1] = 1
	resetTimerFlags[2] = 1
	timers[0].Reset(0)
	timers[1].Reset(0)
	timers[2].Reset(0)
}

func getIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		errlog.Println(err)
	}
	return addrs[1].String()
}

func getIx() int {
	for i, element := range membershipGroup {
		if currHost == element.Host {
			return i
		}
	}
	return -1
}

func getRelativeIx(host string) int {
	localIx := getIx()
	if strings.Compare(membershipGroup[(localIx+1)%len(membershipGroup)].Host, host) == 0 {
		return 1
	} else if strings.Compare(membershipGroup[(localIx+2)%len(membershipGroup)].Host, host) == 0 {
		return 2
	} else if strings.Compare(membershipGroup[(localIx+3)%len(membershipGroup)].Host, host) == 0 {
		return 3
	}
	return -1
}

func sendSyn() {
	for {
		num := len(membershipGroup)
		if num >= MIN_GROUP_SIZE {
			msg := message{getIP(), "SYN", time.Now().Format(time.RFC850)}
			var targetConnections = make([]string, 3)
			targetConnections[0] = membershipGroup[(getIx()+1)%len(membershipGroup)].Host
			targetConnections[1] = membershipGroup[(getIx()+2)%len(membershipGroup)].Host
			targetConnections[2] = membershipGroup[(getIx()+3)%len(membershipGroup)].Host
			sendMsg(msg, targetConnections)
			infolog.Println("SYN messages send: " + time.Now())
		}
		time.Sleep(1 * time.Second)
	}
}

func respondAck(host string) {
	msg := message{currHost, "ACK", time.Now().Format(time.RFC850)}
	var targetConnections = make([]string, 1)
	targetConnections[0] = host

	sendMsg(msg, targetConnections)
}

func gatewayConnect() {
	msg := message{currHost, "Join", time.Now().Format(time.RFC850)}
	var targetConnections = make([]string, 1)
	targetConnections[0] = GATEWAY

	sendMsg(msg, targetConnections)
}

func exitGroup() {
	msg := message{currHost, "Leave", time.Now().Format(time.RFC850)}

	var targetConnections = make([]string, 3)
	for i := 1; i < 4; i++ {
		var targetHostIndex = (getIx() - i) % len(membershipGroup)
		if targetHostIndex < 0 {
			targetHostIndex = len(membershipGroup) + targetHostIndex
		}
		targetConnections[i-1] = membershipGroup[targetHostIndex].Host
	}

	sendMsg(msg, targetConnections)
}

func spreadGroup(msg message) {
	var hostIx = -1
	for i, element := range membershipGroup {
		if msg.Host == element.Host {
			hostIx = i
			break
		}
	}
	if hostIx == -1 {
		return
	}

	updateMG(hostIx, msg)

	var targetConnections = make([]string, 3)
	targetConnections[0] = membershipGroup[(getIx()+1)%len(membershipGroup)].Host
	targetConnections[1] = membershipGroup[(getIx()+2)%len(membershipGroup)].Host
	targetConnections[2] = membershipGroup[(getIx()+3)%len(membershipGroup)].Host

	sendMsg(msg, targetConnections)
}

func broadcastGroup() {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(membershipGroup); err != nil {
		fmt.Println("BroadcastGroup: not able to encode")
		errlog.Println(err)
	}
	for ix, element := range membershipGroup {
		if element.Host != currHost {
			ip, _, _ := net.ParseCIDR(membershipGroup[ix].Host)

			ServerAddr, err := net.ResolveUDPAddr("udp", ip.String()+":10001")
			if err != nil {
				fmt.Println("BroadcastGroup: not able to Resolve server address")
				errlog.Println(err)
			}
			localip, _, _ := net.ParseCIDR(currHost)
			LocalAddr, err := net.ResolveUDPAddr("udp", localip.String()+":0")
			if err != nil {
				fmt.Println("BroadcastGroup: not able to Resolve local address")
				errlog.Println(err)
			}

			conn, err := net.DialUDP("udp", LocalAddr, ServerAddr)
			if err != nil {
				fmt.Println("BroadcastGroup: not able to dial")
				errlog.Println(err)
			}

			_, err = conn.Write(buf.Bytes())
			if err != nil {
				fmt.Println("BroadcastGroup: not able to write to connection")
				errlog.Println(err)
			}

		}
	}
}

func sendMsg(msg message, targetConnections []string) {
	infolog.Println("Sending Message: Host-"+msg.Host+" Status-"+msg.Status+" TS-"+msg.TimeStamp)
	for _,v := range targetConnections{
		infolog.Print(" Target["+v+"] ")
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
		fmt.Println("sendMsg:problem during encoding") 
		errlog.Println(err)
	}

	localip, _, _ := net.ParseCIDR(currHost)
	LocalAddr, err := net.ResolveUDPAddr("udp", localip.String()+":0")
	if err != nil {
		fmt.Println("sendMsg:problem while resolving localip") 
		errlog.Println(err)
	}
	for _, host := range targetConnections {
		if msg.Status == "Leave" || msg.Status == "Failed" {
			fmt.Print("Propagating ")
			fmt.Print(msg)
			fmt.Print(" to :")
			fmt.Println(host)
		}

		ip, _, _ := net.ParseCIDR(host)

		ServerAddr, err := net.ResolveUDPAddr("udp", ip.String()+":10000")
		
		if err != nil {
			fmt.Println("sendMsg:problem while resolving serverip")
			errlog.Println(err)
		}
		conn, err := net.DialUDP("udp", LocalAddr, ServerAddr)
		
		if err != nil {
			fmt.Println("sendMsg:problem while dial")
			errlog.Println(err)
		}
		_, err = conn.Write(buf.Bytes())
		if err != nil {
			fmt.Println("sendMsg:problem while writing to connection")
			errlog.Println(err)
		}
	}
}

func checkTimeStamp(m member) int {
	for _, element := range membershipGroup {
		if m.Host == element.Host {
			t1, _ := time.Parse(time.RFC850, m.TimeStamp)
			t2, _ := time.Parse(time.RFC850, element.TimeStamp)
			if t2.After(t1) {
				element = m
				return 1
			} else {
				break
			}
		}
	}
	return 0
}
