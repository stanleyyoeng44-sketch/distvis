package main

import (
	"sync"
)

type Empty struct{}
type SeqMessage struct{ Seqtotal int64 }

// ralock does not have handling of local concurrency
type ralock struct {
	mu       sync.Mutex
	state    int   //0 means indifferent, 1 means waiting, 2 means holding
	seqtotal int64 // when state==1, holds the seq num
	ch       chan struct{}
}

func (l *ralock) question(seqtotal int64) {
	l.mu.Lock()
	shouldWait := l.state == 2 || (l.state == 1 && l.seqtotal < seqtotal)
	done := l.ch
	//what may happen if we put this outside the lock,
	//we release the lock and then immediately
	// acquire the lock creating a new channel,
	//our comparison was with respect to the old seq,
	// while we wait in the new channel
	l.mu.Unlock()

	//this has to happen outside of locks
	if shouldWait {
		<-done //if done gets closed in between, this will return immediately
	}
	//I'm lucky in that i don't have to decide whether to wait
	//depending on a locked value. which would be really tricky
	//in which we will need cvar
	lc.step()
}

func (l *ralock) lock() error {
	l.mu.Lock()
	l.state = 1
	l.seqtotal = lc.seqtotal()
	l.ch = make(chan struct{})
	l.mu.Unlock()
	var wg sync.WaitGroup
	err := error(nil)
	wg.Add(len(peers))
	for _, client := range peers {
		go func() {
			lc.step()
			seq := SeqMessage{}
			err1 := client.Call("Service.Question", &SeqMessage{Seqtotal: lc.seqtotal()}, &seq)
			lc.mergewith(seq.Seqtotal)
			lc.step()
			if err == nil && err1 != nil {
				err = err1
			}
			wg.Done()
		}()
	}
	wg.Wait()
	if err != nil {
		return err
		//limitation: if error occurs, it will only be reported after all the goroutines finish
	}
	lc.step()
	l.mu.Lock()
	l.state = 2
	l.mu.Unlock()
	return nil
}

func (l *ralock) unlock() {
	l.mu.Lock()
	l.state = 0
	close(l.ch)
	l.mu.Unlock()
}

// this is the wrapper of ralock that we present to applications
type lock struct {
	mu        sync.Mutex
	cond      *sync.Cond
	innerlock bool //0 for vacant, 1 for occupied
	waiting   int
	state     bool //0 for vacant, 1 for occupied, either waiting or executing
	ral       ralock
}

func (l *lock) lock() {
	l.mu.Lock()
	if l.state == false {
		l.state = true
		l.mu.Unlock()
		l.ral.lock()
	} else {
		l.waiting++
		for l.innerlock {
			l.cond.Wait()
		}
		l.waiting--
	}
}

//TODO: implement error handling

func (l *lock) unlock() {
	l.mu.Lock()
	if l.waiting > 0 {
		l.mu.Unlock()
		l.cond.Broadcast()
	} else {
		l.mu.Unlock()
		l.ral.unlock()
		l.mu.Lock()
		l.state = false
		l.mu.Unlock()
	}
}

//what would happen if we do not implement the local locking mechanism well?
//design choice: this lock is goroutine-wise, i.e. a second goroutine on the same node will not be passed the lock to directly, instead it will have to compete after the release.
//question: how should we implement the other mode, where local goroutines hand be handed over the lock directly.
//we need a local boolean to record whether we have lock now, and then if we want to release a clock, we first check that none of our local goroutines is using it, then immediately edit the local variable
//if a local goroutine enters between this. then we can't release.
//we need a local lock for the queue. we lock it up before we check whether its emppty or not, if empty, we set the local variable, then release the lock
//whenever a local goroutine wants to check the local variable, it checks the local lock first.

//later i decided to flip the design choice

//what will a small incorrect lamport seq value do
