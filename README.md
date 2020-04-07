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
