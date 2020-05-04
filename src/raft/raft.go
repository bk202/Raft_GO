package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import "sync"
import "sync/atomic"
import "../labrpc"
import "math/rand"
import "time"
import "fmt"

// import "bytes"
// import "../labgob"



//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

type LogEntry struct{
	Entry interface{}
	Term int
}

const (
	STATE_FOLLOWER = 0
	STATE_CANDIDATE = 1
	STATE_LEADER = 2
)

var verbosity int = 0

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state

	/* ranged in [ 0, len(peers) ) */
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	currentTerm int				  // Current term number
	votedFor int				  // this peer's voted peer to become the next leader
	logEntries []*LogEntry		  // log entries

	// Volatile state on all peers
	commitIndex int				  // The log entry that is being logged on majority of peers
	lastApplied int				  // The last log entry being applied to state machine

	// Volatile on leader only, re-initialized after election
	nextIndex []int 			  // Index of the next log entry for each server
	matchIndex []int			  // matchIndex[i] = commitIndex on follower[i]

	state int 					  // 0=Follower, 1=Candidate, 2=Leader

	/* 
	Number of votes received from followers, initialized to 0, set to 1 upon vote initiation
	*/
	votesReceived int

	heartbeatCh chan interface{}  // Channel for receiving heartbeat messages

	/*
	RPC pushes to this channel to notify main routine of peer becoming leader
	*/
	leaderCh chan interface{}

	/*
	Channel used for notifying main thread of peer has converted back to follower
	*/
	followerCh chan interface{}

	/* 
	Channel for notifying config of a committed entry

	applyCh struct = {
		Command interface{},
		CommandValid bool,
		CommandIndex int
	}
	*/
	applyCh chan ApplyMsg

	/*
	Election timeout duration
	1. an election is initiated upon not receiving 
	an AppendEntries RPC by the end of this duration
	2. refreshed upon receiving any RPC calls
	3. Randomly setted in range of [150, 500] ms
	4. Randomly resetted upon a timeout
	*/

	electionTimeout int

	// Min and max election timeout duration
	timeoutMin int
	timeoutMax int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int = rf.currentTerm
	var isleader bool = (rf.state == STATE_LEADER)
	// Your code here (2A).

	if (verbosity >= 1){
		fmt.Printf("Peer: %d, term: %d, isLeader: %t\n", rf.me, rf.currentTerm, isleader)
	}

	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term int			// term number
	CandidateID int		// Candidate id
	LastLogIndex int	// Index of candidate's last entry
	LastLogTerm int		// term number of candidate's last entry
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term int 			// Peer's term number, for candidate to update itself
	VoteGranted bool 	// Candidate receives vote or not
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).

	/*
	Voting logic:
	1. Grant vote if and only if peer have not voted
	2. Grant vote if candidate's log is more updated than peer:
		- Candidate's term no is greater than peer's term number
		- Candidate's term no is same as peer's term number and 
		log length is longer than peer's log length
	*/

	reply.Term = rf.currentTerm

	rf.mu.Lock()

	// Peer have voted and voted for is not incoming candidate
	if (rf.votedFor != -1){
		reply.VoteGranted = false
	// Compare log versions
	} else {
		// Condition 1.
		if (args.Term > rf.currentTerm){
			reply.VoteGranted = true
			rf.votedFor = args.CandidateID
		// Condition 2.
		} else if (args.Term == rf.currentTerm) && (args.LastLogIndex >= (len(rf.logEntries) - 1)){
			reply.VoteGranted = true
			rf.votedFor = args.CandidateID
		} else{
			reply.VoteGranted = false
		}
	}

	rf.mu.Unlock()

	if (verbosity >= 1){
		fmt.Printf("==========================================\n")
		fmt.Printf("Peer %d vote for Peer %d: %t\n", rf.me, args.CandidateID, reply.VoteGranted)
		fmt.Printf("Peer %d voted for: %d\n", rf.me, rf.votedFor)
		fmt.Printf("==========================================\n")
	}
}

type AppendEntriesArgs struct{
	Term int			// Leader's term number
	LeaderID int		// Leader's peer ID
	PrevLogIndex int	// Index of new log's first entry
	PrevLogTerm int		// Term no. of PrevLogIndex entry
	Entries []*LogEntry // New entries coming from leader (empty for heartbeat messages)
	LeaderCommit int	// Leader's commit index
}

type AppendEntriesReply struct{
	Term int			// Peer's term number for leader to update itself
	Success bool		// True if peer successfully stored new entries
	NextIndex int		// Peer's next log index for leader to keep track of
	MatchIndex int		// Peer's commit index for leader to keep track of
}

func Min(a int, b int) int {
	if (a <= b){
		return a
	}

	return b
}

func Max(a int, b int) int {
	if (a >= b){
		return a
	}

	return b
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply){
	// Send empty struct into heartbeat channel
	rf.heartbeatCh <- struct{}{}

	rf.mu.Lock()

	if (verbosity >= 2){
		fmt.Printf("Peer %d received append entries, No. entries: %d\n", rf.me, len(args.Entries))
	}

	reply.Term = rf.currentTerm
	reply.Success = true
	reply.NextIndex = len(rf.logEntries) - 1

	// Ensure leader term number is correct
	if (args.Term < rf.currentTerm) {
		reply.Success = false 
	} else {
		rf.currentTerm = args.Term
	}

	// Omit heartbeat messages
	if (reply.Success) && (len(args.Entries) != 0){
		// Request fails if peer's log is inconsistent with leader and request
		// for additional entries
		if (args.PrevLogIndex >= len(rf.logEntries)){

			if (verbosity >= 1){
				fmt.Printf("Peer %d, append entries failed, PrevLogIndex: %d\n", rf.me, args.PrevLogIndex)
			}

			reply.Success = false

		} else {
			/*
	 		Reply false if log doesn’t contain an entry at prevLogIndex
	 		whose term matches prevLogTerm
			*/
			if (rf.logEntries[args.PrevLogIndex].Term < args.PrevLogTerm){

				reply.Success = false
				reply.NextIndex = rf.commitIndex

			} else {
				for i:=0; i<len(args.Entries); i++{
					index := args.PrevLogIndex + i + 1

					if (index < len(rf.logEntries)){
						rf.logEntries[index] = args.Entries[i]
					} else {
						rf.logEntries = append(rf.logEntries, args.Entries[i])
					}

					if (verbosity >= 1) {
						fmt.Printf("Peer %d, log[%d]=%d[%d]\n", rf.me, index, rf.logEntries[index].Entry, rf.logEntries[index].Term)
					}
				}

				reply.NextIndex = len(rf.logEntries) - 1
			}
		}

		if (verbosity >= 1){
			fmt.Printf("Peer %d, logs: ", rf.me)
			for i:=0; i<len(rf.logEntries); i++{
				fmt.Printf("%d ", rf.logEntries[i].Entry)
			}
			fmt.Printf(", [%d]\n", args.Entries[len(args.Entries) - 1].Entry)
		}

		if (verbosity >= 2){
			fmt.Printf("Peer %d received logs: %d\n", rf.me, len(args.Entries))
		}

	} else if (reply.Success) {
		// Check for log consistency from heartbeat messages
		if (args.PrevLogIndex >= len(rf.logEntries)){
			reply.Success = false
			reply.NextIndex = rf.commitIndex
		}
		if (args.PrevLogIndex < len(rf.logEntries)) &&
			(rf.logEntries[args.PrevLogIndex].Term < args.PrevLogTerm){
			reply.Success = false
		}

		if (verbosity >= 1) && (!reply.Success){
			fmt.Printf("Peer %d heartbeat commit entries failure, index: %d\n", rf.me, args.PrevLogIndex)
		}
	}

	/*
		If leaderCommit > commitIndex, set 
		commitIndex = min(leaderCommit, index of last new entry)
	*/

	// reply.Success came from heartbeat routine
	if (reply.Success){
		// Only move on if operation is successful
		if (args.LeaderCommit > rf.commitIndex){
			newIndex := Min(args.LeaderCommit, len(rf.logEntries) - 1)
			// newIndex := args.LeaderCommit

			if (newIndex < len(rf.logEntries)){
				// if (verbosity >= 1){
				// 	fmt.Printf("Server: %d, sending commit message, index: %d\n", rf.me, newIndex)
				// }

				for i:=rf.commitIndex+1; i<=newIndex; i++{
					if (verbosity >= 1){
						fmt.Printf("Peer: %d, sending commit message, index: %d[%d], term:%d\n", rf.me, i, rf.logEntries[i].Entry, rf.logEntries[i].Term)
					}
					msg := ApplyMsg{
							CommandValid: true,
							Command: rf.logEntries[i].Entry,
							CommandIndex: i,
					   	   }
					rf.applyCh <- msg
				}

				rf.commitIndex = newIndex
			}
		}
	}

	// reply.NextIndex = len(rf.logEntries) - 1
	reply.MatchIndex = rf.commitIndex

	rf.mu.Unlock()
}

/*
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}
*/

func (rf *Raft) SendAppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply, server int){
	// In case leader's status was changed before entering this call
	if (rf.state != STATE_LEADER) { return }

	// ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	// if !ok { return }

	waitCh := make(chan interface{})

	// Send append entries to peer, if peer doesn't respond within 5o ms, retry rpc
	RetryLoop:
	for {
		go func(waitChannel chan interface{}, args *AppendEntriesArgs, reply *AppendEntriesReply, server int){
			ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)

			// Notify main routine of success
			if ok { waitChannel <- struct{}{} }

		}(waitCh, args, reply, server)

		select {
		// Wait for reply
		case <- waitCh:
			break RetryLoop
		// Try again in 50 milliseconds
		case <- time.After(time.Duration(50) * time.Millisecond):
			continue
		}
	}

	if (reply.Success){

		// Is lock needed here?
		// rf.nextIndex[server] = reply.NextIndex
		// rf.matchIndex[server] = reply.MatchIndex
		rf.nextIndex[server] = Max(rf.nextIndex[server], reply.NextIndex)
		rf.matchIndex[server] = Max(rf.matchIndex[server], reply.MatchIndex)

		if (verbosity >= 2) {
			fmt.Printf("Sever: %d, nextIndex: %d, matchIndex: %d\n", server, reply.NextIndex, reply.MatchIndex)
		}

	} else if (reply.Term <= rf.currentTerm){
		// Resend messages upon failure
		// start := reply.NextIndex
		start := Min(len(rf.logEntries)-1, reply.NextIndex)

		// Use lock here since we don't want the log entries
		// to change while resending
		rf.mu.Lock()

		if (verbosity >= 1){
			fmt.Printf("Leader: %d[%d], resending entries at index: %d to peer %d, peer term:%d\n", rf.me, rf.currentTerm, start, server, reply.Term)
		}

		reArgs := AppendEntriesArgs{}
		reReply := AppendEntriesReply{}

		reArgs.Term = rf.currentTerm
		reArgs.LeaderID = rf.me
		reArgs.PrevLogIndex = start
		reArgs.PrevLogTerm = rf.logEntries[start].Term
		reArgs.Entries = make([]*LogEntry, 0)
		reArgs.LeaderCommit = rf.commitIndex

		// what if start >= len(rf.logEntries)?
		for i:=start+1; i<len(rf.logEntries); i++{
			reArgs.Entries = append(reArgs.Entries, rf.logEntries[i])
			
			if (verbosity >= 2){
				fmt.Printf("Leader %d, log[%d]:%d[%d]\n", rf.me, i, rf.logEntries[i].Entry, rf.logEntries[i].Term)
			}
		}

		rf.mu.Unlock()

		go rf.SendAppendEntries(&reArgs, &reReply, server)
	}
}

func (rf *Raft) BroadCastAppendEntries(args *AppendEntriesArgs){
	for i:=0; i<len(rf.peers); i++{
		if (i == rf.me) { continue }

		reply := AppendEntriesReply{}

		go rf.SendAppendEntries(args, &reply, i)
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) SendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)

	if ok{
		if reply.VoteGranted{
			rf.votesReceived += 1

			if (verbosity >= 1){
				fmt.Printf("Peer %d received vote from %d, votesReceived: %d\n", rf.me, server, rf.votesReceived)
			}
		}

		// majority votes received
		if (rf.votesReceived > (len(rf.peers) / 2)){

			if (rf.state == STATE_LEADER) { return ok }

			rf.leaderCh <- struct{}{}
		}
	}

	return ok
}

func (rf *Raft) BroadCastRequestVote() {
	requestVotesArgs := RequestVoteArgs{
		Term: rf.currentTerm,
		CandidateID: rf.me,
		LastLogIndex: len(rf.logEntries) - 1,
		LastLogTerm: rf.logEntries[len(rf.logEntries) - 1].Term,
	}

	for i:=0; i<len(rf.peers); i++{
		if (i == rf.me) { continue }

		requestVotesReply := RequestVoteReply{}

		go rf.SendRequestVote(i, &requestVotesArgs, &requestVotesReply)
	}
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {

	index := len(rf.logEntries) - 1
	term := rf.currentTerm
	isLeader := (rf.state == STATE_LEADER)

	// Append and broadcast an entry only if peer is leader
	if (isLeader){
		rf.mu.Lock()

		if (verbosity >= 2){
			fmt.Printf("Leader %d received new entry, term: %d\n", rf.me, rf.currentTerm)
		}

		rf.logEntries = append(rf.logEntries, &LogEntry{
													Entry: command,
													Term: rf.currentTerm,
												})
		index = len(rf.logEntries) - 1

		// update leader's next index
		rf.nextIndex[rf.me] = len(rf.logEntries) - 1

		appendEntryArgs := AppendEntriesArgs{}
		appendEntryArgs.Term = rf.currentTerm
		appendEntryArgs.LeaderID = rf.me
		appendEntryArgs.PrevLogIndex = len(rf.logEntries) - 2
		appendEntryArgs.PrevLogTerm = rf.logEntries[len(rf.logEntries) - 2].Term
		appendEntryArgs.LeaderCommit = rf.commitIndex
		appendEntryArgs.Entries = make([]*LogEntry, 0)

		appendEntryArgs.Entries = append(appendEntryArgs.Entries,
										rf.logEntries[len(rf.logEntries) - 1])

		if (verbosity >= 1){
			fmt.Printf("Leader: %d, logs: ", rf.me)
			for i:=0; i<len(rf.logEntries); i++{
				fmt.Printf("%d ", rf.logEntries[i].Entry)
			}
			fmt.Printf("\n")
			// fmt.Printf("Leader %d, num logs: %v\n", rf.me, len(rf.logEntries))
		}

		rf.BroadCastAppendEntries(&appendEntryArgs)

		rf.mu.Unlock()
	}

	return index, term, isLeader
}

/*
type AppendEntriesArgs struct{
	Term int			// Leader's term number
	LeaderID int		// Leader's peer ID
	PrevLogIndex int	// Index of new log's first entry
	PrevLogTerm int		// Term no. of PrevLogIndex entry
	Entries []*LogEntry // New entries coming from leader (empty for heartbeat messages)
	LeaderCommit int	// Leader's commit index
}
*/

func (rf *Raft) HeartBeatRoutine(){
	// Go routine that periodically broadcasts heartbeat message
	for {
		if (rf.dead == 1){ return }
		// Exit heartbeat routine if peer is no longer leader
		if (rf.state != STATE_LEADER) { return }

		heartbeatArgs := AppendEntriesArgs{}
		heartbeatArgs.Term = rf.currentTerm
		heartbeatArgs.LeaderID = rf.me
		// =============================================
		heartbeatArgs.PrevLogIndex = len(rf.logEntries) - 1
		heartbeatArgs.PrevLogTerm = rf.logEntries[len(rf.logEntries) - 1].Term
		// =============================================
		heartbeatArgs.Entries = make([]*LogEntry, 0)
		heartbeatArgs.LeaderCommit = rf.commitIndex

		for i:=0; i<len(rf.peers); i++{
			if (i == rf.me) { continue }

			/*
			 use go routine to send heartbeats, since RPC.Call is a blocking function,
			 it doesn't make sense for leader to wait for heartbeat reply from peers if
			 a peer has died
			*/
			go func(server int){
				heartbeatReply := AppendEntriesReply{}
				rf.peers[server].Call("Raft.AppendEntries", &heartbeatArgs, &heartbeatReply)

				if (heartbeatReply.Success){
					rf.followerCh <- struct{}{}

					rf.nextIndex[server] = heartbeatReply.NextIndex
					rf.matchIndex[server] = heartbeatReply.MatchIndex
				} else if (heartbeatReply.Term > rf.currentTerm){
					// Convert peer back to follower if stale term number detected
					rf.mu.Lock()

					rf.state = STATE_FOLLOWER
					rf.votedFor = -1
					rf.votesReceived = 0

					// Empty leader channel
					for len(rf.leaderCh) > 0 {
						<- rf.leaderCh
					}

					rf.mu.Unlock()

					if (verbosity >= 1){
						fmt.Printf("Leader %d converted back to follower from stale term, peer: %d\n", rf.me, server)
					}
				} else {
					// Stale peer log detected, update peer's log
					reply := AppendEntriesReply{}
					go rf.SendAppendEntries(&heartbeatArgs, &reply, server)
				}

				if (verbosity >= 2){
					fmt.Printf("Next Index: ")
					for i:=0; i<len(rf.peers); i++{
						fmt.Printf("%d ", rf.nextIndex[i])
					}
					fmt.Printf("\n")
				}

			}(i)
		}

		go func(){
			select {
			case <- rf.followerCh:
				return
			case <- time.After(time.Duration(rf.electionTimeout) * time.Millisecond):
				/* 
				Unblock other heartbeat routines
				*/
				// rf.heartbeatCh <- struct{}{}

				if (rf.state != STATE_LEADER) { return }

				// Convert to follower if leader doesn't hear any heart beat reply within eleciton timeout
				rf.mu.Lock()

				rf.state = STATE_FOLLOWER
				rf.votedFor = -1
				rf.votesReceived = 0

				rf.mu.Unlock()

				if (verbosity >= 1){
					fmt.Printf("Leader %d converting back to follower from no heartbeat, term: %d\n", rf.me, rf.currentTerm)
				}

				return
			}
		}()

		// select {
		// case <- rf.heartbeatCh:
		// 	continue

		// case <- time.After(time.Duration(rf.electionTimeout) * time.Millisecond):
		// 	// Convert to follower if leader doesn't hear any heart beat reply within eleciton timeout
		// 	rf.state = STATE_FOLLOWER

		// 	if (verbosity >= 1){
		// 		fmt.Printf("Leader %d converting back to follower\n", rf.me)
		// 	}
		// }

		// Sleep for 50 ms before sending next heart beat
		time.Sleep(50 * time.Millisecond)
	}
}

func (rf *Raft) StartPeer() () {
	for true {
		if (rf.dead == 1){ return }
		// Do job for corresponding states

		// Initiate vote if peer is candidate
		if (rf.state == STATE_CANDIDATE){
			// vote for self
			rf.votedFor = rf.me

			if (verbosity >= 2){
				fmt.Printf("Peer %d, timeout: %d, votesReceived: %d\n", rf.me, rf.electionTimeout, rf.votesReceived)
			}

			// re-random election timer
			rf.electionTimeout = rand.Intn(rf.timeoutMax - rf.timeoutMin) + rf.timeoutMin

			// Minimal vote count is 1, since peer will always vote for self
			rf.votesReceived = 1

			// Broadcast election message
			rf.BroadCastRequestVote()

			/*
			After sending out all vote request RPC's, peer goes back to sleep

			Peer will either:
			1. Peer receives heart beat message from leader and convert to follower
			1. Peer receives leader granted message and conver to leader
			2. Finish sleeping and wake up and convert to follower
			*/

			select{
			case <- rf.heartbeatCh:
				rf.state = STATE_FOLLOWER

				// reset states
				rf.votesReceived = 0
				rf.votedFor = -1

				continue

			case <- rf.leaderCh:
				// launch heartbeat routine to broadcast leader message
				rf.state = STATE_LEADER

				// increment term number on becoming leader
				rf.currentTerm += 1

				if (verbosity >= 1){
					fmt.Printf("***********************************************\n")
					fmt.Printf("Peer %d converted to leader, term:%d\n", rf.me, rf.currentTerm)
					fmt.Printf("***********************************************\n")
				}

				// Launch go routine to clean up leader channel after
				// some time to receive all votes
				go func(){
					time.Sleep(time.Duration(rf.electionTimeout) * time.Millisecond)

					for len(rf.leaderCh) > 0{
						<- rf.leaderCh
					}
				}()

				// start heartbeat routine
				go rf.HeartBeatRoutine()

				continue

			case <- time.After(time.Duration(rf.electionTimeout) * time.Millisecond):
				// Convert to follower upon election duration timeouts
				rf.state = STATE_FOLLOWER

				// reset states
				rf.votesReceived = 0
				rf.votedFor = -1

				continue
			}

		} else if (rf.state == STATE_LEADER){
			// fmt.Printf("Peer %d is leader\n", rf.me)

			// Leader should periodically check for updated commit indexes

			// Check for the next index index N that is on majority of peers
			// and update leader's commit index with N

			votes := make(map[int]int) // <Index, vote count> hash map

			for i:=0; i<len(rf.peers); i++{
				nextIndex := rf.nextIndex[i]

				// check if key exist in map
				if _, ok := votes[nextIndex]; ok {
					votes[nextIndex] += 1
				} else{
					votes[nextIndex] = 1
				}
			}

			commitIndex := -1
			majority := len(rf.peers) / 2

			// Find index s.t. it has majority of votes
			for k, v := range votes {
				if (v > majority){
					commitIndex = k

					// No two commit indexes can receive majority of votes
					break
				}
			}

			if (commitIndex != -1) && (commitIndex > rf.commitIndex){
				// New commit index found, push new committed entries to host/tester
				for i:=rf.commitIndex+1; i<=commitIndex; i++{
					if (verbosity >= 2){
						fmt.Printf("Leader: %d, sending commit message, index: %d[%d], entries: %d\n", rf.me, i, rf.logEntries[i].Entry, len(rf.logEntries))
					}

					msg := ApplyMsg{
							CommandValid: true,
							Command: rf.logEntries[i].Entry,
							CommandIndex: i,
						   }
					rf.applyCh <- msg

				}

				rf.commitIndex = commitIndex

			}

			time.Sleep(50 * time.Millisecond)

		} else if (rf.state == STATE_FOLLOWER){
			/* 
			- Either receive heartbeat and remain as follower
			or sleep for election timeout duration
			*/

			select{
			case <- rf.heartbeatCh:
				// do nothing on receiving heartbeat messages
				continue
			case <- time.After(time.Duration(rf.electionTimeout) * time.Millisecond):
				if (verbosity >= 2){
					fmt.Printf("Peer %d timeout\n", rf.me)
				}
				rf.state = STATE_CANDIDATE
				continue
			}
		}
	}
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
	if (verbosity >= 1){
		logs := make([]interface{}, 0)
		for i:= 0; i<len(rf.logEntries); i++{
			logs = append(logs, rf.logEntries[i].Entry)
		}

		fmt.Printf("Peer %d killed, logs: %v\n", rf.me, logs)
		// fmt.Printf("Peer %d killed, num logs: %v\n", rf.me, len(logs))
	}
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applyCh = applyCh

	// Your initialization code here (2A, 2B, 2C).

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	rf.currentTerm = 0
	rf.votedFor = -1
	rf.votesReceived = 0

	rf.logEntries = make([]*LogEntry, 0)

	// Create initial entry
	rf.logEntries = append(rf.logEntries, &LogEntry{
											Entry: 0,
											Term: rf.currentTerm,
											})
	rf.applyCh <- ApplyMsg{
					CommandValid: true,
					Command: 0,
					CommandIndex: 0,
				  }

	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = make([]int, 0)
	rf.matchIndex = make([]int, 0)

	rf.timeoutMin = 150
	rf.timeoutMax = 300
	rf.electionTimeout = rand.Intn(rf.timeoutMax - rf.timeoutMin) + rf.timeoutMin

	if (verbosity >= 1){
		fmt.Printf("Peer: %d, Timeout: %d\n", rf.me, rf.electionTimeout)
	}

	rf.state = STATE_FOLLOWER

	rf.heartbeatCh = make(chan interface{})
	rf.leaderCh = make(chan interface{}, 100)
	rf.followerCh = make(chan interface{})

	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))

	atomic.StoreInt32(&rf.dead, 0)

	go rf.StartPeer()

	return rf
}
