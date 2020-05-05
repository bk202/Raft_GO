# A Raft consensus algorithm implementation in GO

## Part 2A

### 2A consists of three parts:
1. Initial election
- All peers initialized as follower and will convert to candidate upon election timeout
- Candidate converts to leader upon receiving majority votes and launches a go routine to periodically broadcast heartbeat RPC messages to followers
- A safe gap between heartbeat messages is 50 ms

2. Leader disconnection
- Follower converts to candidate if it doesn't receive any heartbeat message after its sleep duration timeouts
- Candidate should convert back to follower if it doesn't receive sufficient votes during the election period

3. Stale leader rejoin
- Stale leader should realize a stale self after rejoining cluster via heartbeat replies
- Stale leader should convert to follower and update its fields

### Things to note in 2A
- The central idea of raft in Golang is implemented with **Go routine** + **Select** + **Channel**
- Use **Select** to block main routine and use **Channel** to receive signals from RPC to unblock main routine
- Two channels `heartbeatCh` and `leaderCh` are used to notify main routine of RPC signals:

  `heartbeatCh`: Used to notify main routine of receiving heartbeat message from leader
  
  `leaderCh`: Used to notify main routine of becoming leader

- Labrpc simulates a lossy network environment, therefore it is not safe to assume target peers will always receive RPC messages
- 2A does not care about log manipulations
- Remember to reset peer fields upon state change

## Part 2B

### 2B consists of _ tests
1. Basic agreement
- The basic agreement test exmaines whether majority of peers successfully agree on an entry's commitment or not
- Tester uses API cfg.Start() to initiate an entry commitment to leader
- Leader sends new entry to other peers, peers reply with update success or failure along with peer's next index and commit index
- Leader should periodically checks for new commit index, upon discovering a new commit index, leader will notify peers and tester of new commit index
- New commit index satisfies the following condition in the paper: If there exists an N such that N > commitIndex, a majority
of matchIndex[i] ≥ N, and log[N].term == currentTerm: set commitIndex = N

2. RPC Byte count
- Ensures if tester sends entry of N byte to leader, all peers should hold an entry of exactly N bytes at the corresponding index

3. Agreement despite follower disconnection
- System should still be able to commit an entry despite some followers have lost connection with majority peers remain connected
- Disconnected followers should be updated of their out-dated log entries once they reconnect to the system
- In AppendEntries RPC, if args.PrevLogIndex exceeds peer's log entries length, request leader resend entries starting from peer's commit index
- If args.PrevLogIndex < peer's log entries length, overwrite peer's log entries by incoming entries
- If the term number at peer's logs[args.PrevLogIndex] > args.logs[args.PrevLogIndex], refuse AppendEntries and notify leader of stale log entries

4. No agreement if too many followers disconnect
- No log entries should be committed if system does not maintain majority peers connected
- Similar to test 3, leader should still update follower's logs once they reconnect to the system

5. Conccurrent Start()
- System must be capable of handling synchronous Start() calls, this is done via peer's mutex lock

6. Rejoin of partitioned leader
- By disconnecting a leader from system, the system should elect a new leader for next term
- Old leader should convert to follower under one of two conditions:
 
  - Outdated term number from append entry replies
  - Does not receive any heartbeat reply from heartbeat routine

- Upon old leader reconnection, old leader will rejoin as a follower and receive log entry updates from new leader (either via heartbeat routine or new append entries)

7. Leader backs up quickly over incorrect follower logs
- Reconnected leader should discover a stale self upon receiving append entry replies from peers QUICKLY
- The difficult part of this test is actually the long re-election time coming from frequent leader disconnections, the test fails if the entire test runs beyond 120s (which happens sometimes, but rare)

8. RPC counts aren't too high
- One() retries if tester doesn't receive a log committment within 10 seconds (which happens quite frequently, since the simulated network is lossy)
- In consequence, system produces duplicated logs for the same entry due to tester's retry
- This test ensures system doesn't produce too many duplicated logs (4 times of logs sent)
- To ensure a log gets committed within 10s, leader should implement a retry mechanism within it's own send append entries API
- Leader should retry sending append entries to a peer if it doesn't hear reply within some time (50 ms in this sytem)

### Things to note in 2B
- If an log entry at index I on two peers with the same term number, it is guaranteed the two entries are identical (Since the system ensures no two leaders will appear in the same term, hence the two entries must have came from the same leader, and a leader will never overwrite it's own logs, unless it becomes a follower)
- Heartbeat routine should also be responsible for discovering a peer's stale log entries and provide update to peer
- Remember to kill goroutines via raft's builtin Killed() method!
- Use lock to prevent sending multiple logs at once
- Channels: if a thread waits on a channel and two other threads pushed to the same channel, the main thread will be unblocked twice if it waits on the channel twice
- heartbeat routine should be responsible for maintaining log consistency
