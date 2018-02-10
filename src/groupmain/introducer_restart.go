package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

//Called by introducer after receiving a join message. Compares timestamp of the message and.
//the membershiplist. If timestamp in memberhiplist is more recent than message time, return 1
// otherwise return 0
func checkTimeStamp(m member) int {
	for _, element := range membershipList {
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

//Helper function to write membershipList to file
func writeMLtoFile() {
	if strings.Compare(currHost, INTRODUCER) == 0 {
		f, err := os.Create(FILE_PATH)
		errorCheck(err)
		defer f.Close()
		writer := bufio.NewWriter(f)
		for _, element := range membershipList {
			fmt.Fprintln(writer, element.Host)
		}
		writer.Flush()
	}
}

//Helper function to convert file to membershiplist
func fileToML() {
	currTime := time.Now().Format(time.RFC850)
	file, err := os.Open(FILE_PATH)
	errorCheck(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		node := member{scanner.Text(), currTime}
		if strings.Compare(node.Host, INTRODUCER) != 0 {
			membershipList = append(membershipList, node)
		}
	}
	validFlags = make([]int, len(membershipList))
	for i := 0; i < len(membershipList); i++ {
		validFlags[i] = 0
	}
}

//Function for introducer to send "isAlive" messages to VM's in it's membershiplist after reboot
//This is to check validity of local membershipList is introducer crashes and needs to restart
func checkMLValid() {
	localip, _, _ := net.ParseCIDR(currHost)
	LocalAddr, err := net.ResolveUDPAddr("udp", localip.String()+":0")
	errorCheck(err)

	for index, element := range membershipList {
		msg := message{currHost, "isAlive", time.Now().Format(time.RFC850)}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
			errorCheck(err)
		}
		if element.Host != currHost {
			go func(LA *net.UDPAddr, host string, bufMsg bytes.Buffer) {
				ip, _, _ := net.ParseCIDR(host)

				ServerAddr, err := net.ResolveUDPAddr("udp", ip.String()+":10000")
				errorCheck(err)

				conn, err := net.DialUDP("udp", LA, ServerAddr)
				errorCheck(err)
				for i := 0; i < 5; i++ {

					_, err = conn.Write(bufMsg.Bytes())
					errorCheck(err)
					time.Sleep(50 * time.Millisecond)
				}
			}(LocalAddr, membershipList[index].Host, buf)
		}
	}
}

//After sending isAlive messages and waiting for a yup response, introducer updates
// it's membershipList according to the validFlags array. Indexes with value 0 means
// VM didn't respond. 1 means VM responded.
func checkValidFlags() {
	time.Sleep(3 * time.Second)
	i := 0
	for j := 0; j < len(validFlags); j++ {
		if validFlags[j] == 0 && membershipList[i].Host != INTRODUCER {
			infoCheck(membershipList[i].Host + " Left or failed")
			membershipList = append(membershipList[:i], membershipList[i+1:]...)
		} else {
			i++
		}
	}
}
