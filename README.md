# Group-Membership

The distributed group membership maintains an active membership list of all the nodes in the network. The list is updated when a node joins, leaves or fails. The project uses hardcoded Gateway node. The gateway node has to be changes according to your network.

###### How to get the repo and run
Follow the following steps on **all machines**
- The project has 3 packages, groupmain contains the main entry point of program, grepserver is the one started to listen to grep requests and utils holds utility functions.
- First clone the project on Gateway nodes (specified in membseship.go)
- Clone the repo
```
~/> git clone https://github.com/aditya-sathe/Group-Membership.git
```
- This will create a folder `Group-Membership` in the current direcory.
- Set the GOPATH the the location of projects folder example `export GOPATH=$HOME/Group-Membership`
- Change the dir to `Group-Memebership` folder and run the `membership.go` from there. Since the project uses relative paths, it is important that the current working directory should be `Group-Membership`
```
~/Group-Memebership> go run src/groupmain/membership.go
```
- Now you can see the options available to join, leave etc.
- First hit option '3' to join the nework. Then hit '1' to see the updated membership list.
- Debug Logs are generated at src/logs/logfile.log.
- Option '5' will grep logs on all machines. You can specify the stanard linux grep options and keywords including regex.

###### Assumptions 
- The gateway node cannot crash or restart.   
