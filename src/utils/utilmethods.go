package utils

import (
	"os/exec"
	"fmt"
	"bytes"
	"io/ioutil"
	"net"
	"encoding/gob"
	"time"
)

const LOG_FILE = "logfile.log"

/*
 * Executes grep in unix shell
 */
func ExecGrep(cmdArgs []string, logName string, machineName string) string {
	
	cmdArgs = append(cmdArgs, logName) 
	fmt.Println("Complete String: ", cmdArgs)
	
	cmdOut, cmdErr := exec.Command("grep", cmdArgs...).Output()

	results := ""
	//check if there is any error in our grep
	if cmdErr != nil {
		fmt.Println("ERROR WHILE READING")
		fmt.Println(cmdErr)
	}

	if len(cmdOut) > 0 {
		results = machineName + "-" + logName + "\n" + string(cmdOut)
	} else {
		results = "No matching patterns found in " + machineName
	}
	return results
}

/*
 * Sends a message to a server, and returns the result into a channel
 */
func SendToServer(ipAddr string, message []string, c chan string) {
	
	conn, err := net.DialTimeout("tcp", ipAddr, time.Duration(1)*time.Second)
	if err != nil {
		c <- err.Error()
		return
	}
	
	defer conn.Close()
	
	// convert string array to bytes
    buf := &bytes.Buffer{}
    gob.NewEncoder(buf).Encode(message[1:])
    messageBytes := buf.Bytes()  
    // write bytes to the socket
	_, err = conn.Write(messageBytes)
	if err != nil {
		c <- err.Error()
		return
	}

	result, err := ioutil.ReadAll(conn)
	if err != nil {
		c <- err.Error()
		return
	}

	c <- string(result)
}

/*  
 * Returns the non loopback local IP of the host
 */
func GetLocalIP() string {
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return "Error getting IP address"
    }
    for _, address := range addrs {
        // check the address type and if it is not a loopback the display it
        if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() != nil {
                return ipnet.IP.String()
            }
        }
    }
    return ""
}
