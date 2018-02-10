package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"time"
)

//Handles connection protocol and writes message to server
//Takes a message and the IP's of the VM's to send the message to as a slice of strings
//Messages are encoded using golang's gobbing protocol
func sendMsg(msg message, targetHosts []string) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
		errorCheck(err)
	}

	localip, _, _ := net.ParseCIDR(currHost)
	LocalAddr, err := net.ResolveUDPAddr("udp", localip.String()+":0")
	errorCheck(err)

	for _, host := range targetHosts {
		if msg.Status == "Adios" || msg.Status == "Failed" {
			fmt.Print("Propagating ")
			fmt.Print(msg)
			fmt.Print(" to :")
			fmt.Println(host)
		}

		ip, _, _ := net.ParseCIDR(host)

		ServerAddr, err := net.ResolveUDPAddr("udp", ip.String()+":10000")
		errorCheck(err)

		conn, err := net.DialUDP("udp", LocalAddr, ServerAddr)
		errorCheck(err)

		randNum := rand.Intn(100)
		fmt.Print("Random number = ")
		fmt.Println(randNum)
		if !((msg.Status == "SYN" || msg.Status == "ACK" || msg.Status == "Failed" || msg.Status == "Adios") && randNum < PACKET_LOSS) {
			_, err = conn.Write(buf.Bytes())
			errorCheck(err)
		} else {
			packets_lost++
			fmt.Print(packets_lost)
			fmt.Print(" Message failed to send becaue of packet loss: ")
			fmt.Println(msg)
		}
	}
}

//VM's ping the next 2 members in the membershipList for an ACK
func sendSyn() {
	for {
		N := len(membershipList)
		if N >= MIN_HOSTS {
			msg := message{getIP(), "SYN", time.Now().Format(time.RFC850)}
			var targetHosts = make([]string, 2)
			targetHosts[0] = membershipList[(getIndex()+1)%len(membershipList)].Host
			targetHosts[1] = membershipList[(getIndex()+2)%len(membershipList)].Host

			sendMsg(msg, targetHosts)
		}
		time.Sleep(1 * time.Second)
	}
}

//Called when a VM receives a syn. An ack is sent back to the corresponding IP
func sendAck(host string) {
	msg := message{currHost, "ACK", time.Now().Format(time.RFC850)}
	var targetHosts = make([]string, 1)
	targetHosts[0] = host

	sendMsg(msg, targetHosts)
}

//Message sent to introducer from a VM to connect to the group
func connectToIntroducer() {
	msg := message{currHost, "Joining", time.Now().Format(time.RFC850)}
	var targetHosts = make([]string, 1)
	targetHosts[0] = INTRODUCER

	sendMsg(msg, targetHosts)
}

//Message sent to previous 2 VM's in membershiplist notifying that the VM is leaving the group
func leaveGroup() {
	msg := message{currHost, "Adios", time.Now().Format(time.RFC850)}

	var targetHosts = make([]string, 2)
	for i := 1; i < 3; i++ {
		var targetHostIndex = (getIndex() - i) % len(membershipList)
		if targetHostIndex < 0 {
			targetHostIndex = len(membershipList) + targetHostIndex
		}
		targetHosts[i-1] = membershipList[targetHostIndex].Host
	}

	sendMsg(msg, targetHosts)
}

//Response from VM's to the introducer in response to isAlive. Sent to indicate to the INTRODUCER
//that the VM is still connected to the group so the INRODUCER doesn't delete it from its membershiplist
func yup() {
	msg := message{currHost, "yup", time.Now().Format(time.RFC850)}
	var targetHosts = make([]string, 1)
	targetHosts[0] = INTRODUCER

	sendMsg(msg, targetHosts)

}

//Called when messages (such as when a member leaves or fails) needs to be propagated to the rest
//of the group. Messages are propagated to the next two members in the membershipList
//If the member is not in the local membershipList then the message is ignored (this would happen
//when a VM has already received a message and made the changes)
//If the member is in the membershipList, updateML is called to compare the timestamps and updates the
//membershipList is necessary.
//The message is then propagated to the next two VM's in the membershipList
func propagateMsg(msg message) {
	var hostIndex = -1
	for i, element := range membershipList {
		if msg.Host == element.Host {
			hostIndex = i
			break
		}
	}
	if hostIndex == -1 {
		return
	}

	msgCheck(msg)
	updateML(hostIndex, msg)

	var targetHosts = make([]string, 2)
	targetHosts[0] = membershipList[(getIndex()+1)%len(membershipList)].Host
	targetHosts[1] = membershipList[(getIndex()+2)%len(membershipList)].Host

	sendMsg(msg, targetHosts)
}

//Called by introducer if a new member joins group. Sends a membershipList to each member in membershipList
func sendList() {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(membershipList); err != nil {
		errorCheck(err)
	}
	for index, element := range membershipList {
		if element.Host != currHost {
			ip, _, _ := net.ParseCIDR(membershipList[index].Host)

			ServerAddr, err := net.ResolveUDPAddr("udp", ip.String()+":10001")
			errorCheck(err)

			localip, _, _ := net.ParseCIDR(currHost)
			LocalAddr, err := net.ResolveUDPAddr("udp", localip.String()+":0")
			errorCheck(err)

			conn, err := net.DialUDP("udp", LocalAddr, ServerAddr)
			errorCheck(err)

			_, err = conn.Write(buf.Bytes())
			errorCheck(err)
		}
	}
}
