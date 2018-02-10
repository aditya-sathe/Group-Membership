package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"sync"
	"strings"
	"time"
	"grepserver"
	"utils"
)

//IP address, as a string, for the introducer - the VM that outher VM's will ping to join the group
const INTRODUCER = "172.22.149.18/23"

//File path for membershipList. Only applies to INTRODUCER
const FILE_PATH = "MList.txt"

//Minimum number of VM's in the group before Syn/Ack-ing begins
const MIN_HOSTS = 5

//Maximum time a VM will wait for an ACK from a machine before marking it as failed
const MAX_TIME = time.Millisecond * 2500

//IP of the local machine as a string
var currHost string

//Flag to indicate if the machine is currently connected to the group
//1 = machine is connected, 0 = machine is not connected
var isConnected int

//Mutex used for membershipList and timers
var mutex = &sync.Mutex{}

//Timers for checking last ack - used in checkLastAck function
//When timer reaches 0, corresponding VM is marked as failed unless reset flags are 1
var timers [2]*time.Timer

//resetFlags indicate whether timer went off or were stopped to reset the timers
//1 = timers were forcefully stopped
var resetFlags [2]int

//Contains all members connected to the group
var membershipList = make([]member, 0)

//Used if introducer crashes and reboots using a locally stored membership list
var validFlags []int

//type and functions used to sort membershipLists
type memList []member

func (slice memList) Len() int           { return len(slice) }
func (slice memList) Less(i, j int) bool { return slice[i].Host < slice[j].Host }
func (slice memList) Swap(i, j int)      { slice[i], slice[j] = slice[j], slice[i] }

//For logging
var logfile *os.File
var errlog *log.Logger
var infolog *log.Logger
var joinlog *log.Logger
var leavelog *log.Logger
var faillog *log.Logger
var emptylog *log.Logger

//For simulating packet loss in percent
const PACKET_LOSS = 0

var packets_lost int

//struct for information sent from client to server
type message struct {
	Host string
	//port string
	Status    string
	TimeStamp string
}

//Information kept for each VM in the group, stored in membershipList
type member struct {
	Host      string
	TimeStamp string
}

//var startup = flag.Int("s", 0, "Value to decide if startup node")

func main() {
	fmt.Println("Harambe")
	initializeVars()

	//start servers to receive connections for messages and membershipList
	//updates from the introducer when new VM's join
	go messageServer()
	go membershipServer()
	go grepserver.StartGrepServer()

	//Reader to take console input from the user
	reader := bufio.NewReader(os.Stdin)

	//If VM is the introducer, follow protocol for storing membershipList as a local file
	if currHost == INTRODUCER {
		//If membershipList file exists, check is user wants to restart server using
		//the file or start a new group
		if _, err := os.Stat(FILE_PATH); os.IsNotExist(err) {
			writeMLtoFile()
		} else {
			fmt.Println("\nA membership list exists in the current directory.")
			fmt.Println("Would you like to restart the connection using the existing membership list? y/n\n")
			input, _ := reader.ReadString('\n')
			switch input {
			case "y\n":
				infoCheck("Restarting master...")
				fileToML()
				checkMLValid()
				checkValidFlags()
				writeMLtoFile()
				sendList()
			case "n\n":
				writeMLtoFile()
			default:
				fmt.Println("Invalid command")
			}
		}
	}

	//Start functions sending syn's and checking for ack's in seperate threads
	go sendSyn()
	go checkLastAck(1)
	go checkLastAck(2)

	takeUserInput(reader)
}

//Creates a server to respond to messages
func messageServer() {
	ServerAddr, err := net.ResolveUDPAddr("udp", ":10000")
	errorCheck(err)

	ServerConn, err := net.ListenUDP("udp", ServerAddr)
	errorCheck(err)
	defer ServerConn.Close()

	buf := make([]byte, 1024)

	for {
		msg := message{}
		n, _, err := ServerConn.ReadFromUDP(buf)
		err = gob.NewDecoder(bytes.NewReader(buf[:n])).Decode(&msg)
		errorCheck(err)
		switch msg.Status {
		/* 	if joining, create a member with the host and current time, add member to membershiplist,
		sort the membershiplist, and sent list to all members in membershipList (only the introducer will receive
		joing message.*/
		case "Joining":
			msgCheck(msg)
			node := member{msg.Host, time.Now().Format(time.RFC850)}
			if checkTimeStamp(node) == 0 {
				mutex.Lock()
				resetTimers()
				membershipList = append(membershipList, node)
				sort.Sort(memList(membershipList))
				mutex.Unlock()
			}
			go writeMLtoFile()
			//propagateMsg(msg)
			sendList()
		/*	if syn, send an ACK back to to the ip that sent the syn*/
		case "SYN":
			fmt.Print("Syn received from: ")
			fmt.Println(msg.Host)
			sendAck(msg.Host)
		/*	if ack, check if ip that sent the message is either (currIndex + 1)%N or (currIndex + 2)%N
			and reset the corresponding timer to MAX_TIME*/
		case "ACK":
			if msg.Host == membershipList[(getIndex()+1)%len(membershipList)].Host {
				fmt.Print("ACK received from ")
				fmt.Println(msg.Host)
				timers[0].Reset(MAX_TIME)
			} else if msg.Host == membershipList[(getIndex()+2)%len(membershipList)].Host {
				fmt.Print("ACK received from ")
				fmt.Println(msg.Host)
				timers[1].Reset(MAX_TIME)
			}
		/*	if message status is failed, propagate the message (timers will be taken care of in checkLastAck*/
		case "Failed":
			propagateMsg(msg)
		/*	if a node leaves, propagate message and reset timers*/
		case "Adios":
			mutex.Lock()
			resetTimers()
			propagateMsg(msg)
			mutex.Unlock()
		/*	isAlive message is sent from introducer. Send a yup message back to let introducer know that that VM is
			still in the group*/
		case "isAlive":
			yup()
		/*	received by introducer. valid flags will initially contain an array of 0's corresponding to each member
			in the membershipList. The value will be updated to 1 if a yup is received from the corresponding VM*/
		case "yup":
			for i, element := range membershipList {
				if msg.Host == element.Host {
					validFlags[i] = 1
					break
				}
			}

		}
	}
}

//Server to receieve updated membershipList from introducer if a new member joins
func membershipServer() {
	ServerAddr, err := net.ResolveUDPAddr("udp", ":10001")
	errorCheck(err)

	ServerConn, err := net.ListenUDP("udp", ServerAddr)
	errorCheck(err)
	defer ServerConn.Close()

	buf := make([]byte, 1024)

	for {
		mL := make([]member, 0)
		n, _, err := ServerConn.ReadFromUDP(buf)
		err = gob.NewDecoder(bytes.NewReader(buf[:n])).Decode(&mL)
		errorCheck(err)

		//restart timers if membershipList is updated
		mutex.Lock()
		resetTimers()
		membershipList = mL
		mutex.Unlock()

		var msg = "New VM joined the group: \n\t["
		var N = len(mL) - 1
		for i, host := range mL {
			msg += "(" + host.Host + " | " + host.TimeStamp + ")"
			if i != N {
				msg += ", \n\t"
			} else {
				msg += "]"
			}
		}
		infoCheck(msg)
	}
}

//VM's are marked as failed if they have not responded with an ACK within MAX_TIME
//2 checkLastAck calls persist at any given time, one to check the VM at (currIndex + 1)%N and one to
//check the VM (currIndex + 2)%N, where N is the size of the membershipList
//relativeIndex can be 1 or 2 and indicates what VM the function to watch
//A timer for each of the two VM counts down from MAX_TIME and is reset whenever an ACK is received (handled in
// messageServer function.
//Timers are reset whenever the membershipList is modified
//The timer will reach 0 if an ACK isn't received from the corresponding VM
// within MAX_TIME, or the timer is reset. If a timer was reset, the corresponding resetFlag will be 1
// and indicate that checkLastAck should be called again and that the failure detection should not be called
//If a timer reaches 0 because an ACK was not received in time, the VM is marked as failed and th message is
//propagated to the next 2 VM's in the membershipList. Both timers are then restarted.
func checkLastAck(relativeIndex int) {
	//Wait until number of members in group is at least MIN_HOSTS before checking for ACKs
	for len(membershipList) < MIN_HOSTS {
		time.Sleep(100 * time.Millisecond)
	}

	//Get host at (currIndex + relativeIndex)%N
	host := membershipList[(getIndex()+relativeIndex)%len(membershipList)].Host
	fmt.Print("Checking ")
	fmt.Print(relativeIndex)
	fmt.Print(": ")
	fmt.Println(host)

	//Create a new timer and hold until timer reaches 0 or is reset
	timers[relativeIndex-1] = time.NewTimer(MAX_TIME)
	<-timers[relativeIndex-1].C

	/*	3 conditions will prevent failure detection from going off
		1. Number of members is less than the MIN_HOSTS
		2. The target host's relative index is no longer the same as when the checkLastAck function was called. Meaning
		the membershipList has been updated and the checkLastAck should update it's host
		3. resetFlags for the corresponding timer is set to 1, again meaning that the membership list was updated and
		checkLastack needs to reset the VM it is monitoring.*/
	mutex.Lock()
	if len(membershipList) >= MIN_HOSTS && getRelativeIndex(host) == relativeIndex && resetFlags[relativeIndex-1] != 1 {
		msg := message{membershipList[(getIndex()+relativeIndex)%len(membershipList)].Host, "Failed", time.Now().Format(time.RFC850)}
		fmt.Print("Failure detected: ")
		fmt.Println(msg.Host)
		propagateMsg(msg)

	}
	//If a failure is detected for one timer, reset the other as well.
	if resetFlags[relativeIndex-1] == 0 {
		fmt.Print("Force stopping timer")
		fmt.Println(relativeIndex)
		resetFlags[relativeIndex%2] = 1
		timers[relativeIndex%2].Reset(0)
	} else {
		resetFlags[relativeIndex-1] = 0
	}

	mutex.Unlock()
	go checkLastAck(relativeIndex)

}

/*
 * Take user inputs for all options
 */
func takeUserInput(reader *bufio.Reader){
		for {
		fmt.Println("1 -> Print membership list")
		fmt.Println("2 -> Print self ID")
		fmt.Println("3 -> Join group")
		fmt.Println("4 -> Leave group")
		fmt.Println("5 -> Grep node logs\n")
		input, _ := reader.ReadString('\n')
		switch input {
		case "1\n":
			for _, element := range membershipList {
				fmt.Println(element)
			}
		case "2\n":
			fmt.Println(currHost)
		case "3\n":
			if currHost != INTRODUCER && isConnected == 0 {
				fmt.Println("Joining group")
				connectToIntroducer()
				infoCheck(currHost + " is connecting to master")
				isConnected = 1
			} else {
				fmt.Println("I AM THE MASTER")
			}
		case "4\n":
			if isConnected == 1 {
				fmt.Println("Leaving group")
				leaveGroup()
				infoCheck(currHost + " left group")
				os.Exit(0)

			} else {
				fmt.Println("You are currently not connected to a group")
			}
		case "5\n":
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
func grepClient(reader *bufio.Reader){
	
	fmt.Println("Usage: -options keywordToSearch")
	fmt.Println("-options: available in linux grep command")
	
	input, _ := reader.ReadString('\n')
	input = strings.TrimSuffix(input, "\n")
	serverInput := strings.Split(input, " ") 
	c := make(chan string)
	// Send data to every server in membershipList
	for _, element := range membershipList {
		localip, _, _ := net.ParseCIDR(element.Host)	
		fmt.Println("ParseCIDR: " , localip.String()) 
		go utils.SendToServer(localip.String()+":"+grepserver.PORT, serverInput, c)
	}

	// Print results from server
	for i, _ := range membershipList {
		serverResult := <-c
		fmt.Println(serverResult)
		fmt.Printf("END----------%d\n", i)	
	}
}
