# Paxos replicated state machine KV
## Observation scenarioA
![](image.png)

this screenshot shows the process of one log. First we use Paxos to get a majority vote agreeing on the log, next the proposer who receives majority would take the obligation to tell everyone that this slot has been decided upon.

Only after a node receives the decision notice, does it treat the slot as decided. Before that, even if it has accepted a value, it can still send out prepare requests.

This is very counterintuitive in our implementation, the general question is that, if someone has already accepted a value for that, can it propose another value for it? Generally this other value will not be adopted, because he himself will reply back with an ack to the preparation message, attached with his own accepted value. 


retrying mechanism:
When a node wants to append some action to the log, it starts the pipeline shown in the first image, however there are two kinds of major 'failures':
2. 

When a node 


imagine in paxos, 1 node has the biggest ballot with an accepted value of A, the others all have a smaller ballot with an accepted value of B. then for every prepare, since it will select the value among the attached values in the replies with the largest ballot, it will always be A, and then eventually everyone will agree on A.


Based on that, let's construct this case to test on our paxos code. If every