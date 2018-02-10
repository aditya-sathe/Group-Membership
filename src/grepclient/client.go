package grepclient 

import (
	"bufio"
	"fmt"
	"os"
	"time"
	"utils"
)

const (
	SERVER_PORT = "8008"
	SERVER_LIST = "serverlist.prop"
)

func grepClient() {
	ipList := []string{}
	
	file, err := os.Open(SERVER_LIST)
	
	if( err!= nil){
		fmt.Errorf("Error reading file: ", SERVER_LIST)
		os.Exit(1)
	}
	
	defer file.Close()	
	scanner := bufio.NewScanner(file)

	// Compile list of ip address from serverlist.prop
	for scanner.Scan() {
		var ip = scanner.Text()
		ip = ip + ":" + SERVER_PORT
		ipList = append(ipList, ip)
	}
	
	if err := scanner.Err(); err !=nil{
		fmt.Errorf("Error scanning file: %s - %s", SERVER_LIST, err)
		os.Exit(1)
	}
	
	t0 := time.Now()

	if len(os.Args) < 2 {
		fmt.Println("ERROR: Not enough arguments.")
		fmt.Println("Usage: client.go -options keywordToSearch")
		fmt.Println("		-options: available in linux grep command")
		os.Exit(1)
	}
	 
	c := make(chan string)

	serverInput := os.Args 

	// Send data to every server in serverlist.prop
	for _, v := range ipList {
		go utils.SendToServer(v, serverInput, c)
	}

	// Print results from server
	for i, _ := range ipList {
		serverResult := <-c
		fmt.Println(serverResult)
		fmt.Printf("END----------%d\n", i)	
	}

	t1 := time.Now()
	fmt.Print("Grep search took: ")
	fmt.Println(t1.Sub(t0))
}

