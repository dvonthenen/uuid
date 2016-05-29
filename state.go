package uuid

/****************
 * Date: 14/02/14
 * Time: 7:43 PM
 ***************/

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"log"
	"net"
	"sync"
	"os"
	"fmt"
)

// **************************************************** State


var (
	posixUID      = uint32(os.Getuid())
	posixGID      = uint32(os.Getgid())
)

// Run this method before any calls to NewV1 or NewV2 to save the state to
// YOu must implement the uuid.Saver interface and are completely resposnible
// for the non volaitle storage of the state
func SetupSaver(pStateStorage Saver) {
	state.Do(func() {
		defer state.init()
		state.Lock()
		defer state.Unlock()
		state.Saver = pStateStorage
	})
}

// Use this interface to setup a non volatile store within your system
// if you wish to have  v1 and 2 UUIDs based on your node id and constant time
// it is highly recommended to implemment this
// You could use FileSystemStorage, the default is to generate random sequences
type Saver interface {
	// Read is run once, use this to setup your UUID state machine
	// Read should also return the UUID state from the non volatile store
	Read() (error, Store)

	// Save saves the state to the non volatile store and is called only if
	Save(*Store)
}

// The storage data to ensure continuous running of the UUID generator between restarts
type Store struct {

	// the last time UUID was saved
	Timestamp

	// an iterated value to help ensure different
	// values across the same domain
	Sequence

	// the last node which saved a UUID
	Node
}

func (o Store) String() string {
	return fmt.Sprint(o.Timestamp, o.Sequence, o.Node)
}


type generator struct {

	sync.Mutex
	sync.Once

	Saver
	*Store

	Spin
}

func (o *generator) read() *Store {

	// From a system-wide shared stable store (e.g., a file), read the
	// UUID generator state: the values of the timestamp, clock sequence,
	// and node ID used to generate the last UUID.
	o.Do(o.init)

	// Save the state (current timestamp, clock sequence, and node ID)
	// back to the stable store
	defer o.save()

	// Obtain a lock
	o.Lock()
	defer o.Unlock()

	// Get the current time as a 60-bit count of 100-nanosecond intervals
	// since 00:00:00.00, 15 October 1582.
	now := o.Spin.next()

	// If the last timestamp is later than
	// the current timestamp, increment the clock sequence value.
	if now < o.Timestamp {
		o.Sequence++
	}

	o.Timestamp = now

	return o.Store
}

func (o *generator) init() {
	// From a system-wide shared stable store (e.g., a file), read the
	// UUID generator state: the values of the timestamp, clock sequence,
	// and node ID used to generate the last UUID.
	var (
		storage Store
		err error
	)

	// Save the state (current timestamp, clock sequence, and node ID)
	// back to the stable store.
	defer o.save()

	o.Lock()
	defer o.Unlock()

	if (o.Saver != nil) {
		err, storage = o.Read()

		if err !=nil {
			o.Saver = nil
		}
		o.Spin.Resolution = defaultSpinResolution
	}

	// Get the current time as a 60-bit count of 100-nanosecond intervals
	// since 00:00:00.00, 15 October 1582.
	now := o.Spin.next()

	//  Get the current node ID.
	node := getHardwareAddress()

	// If the state was unavailable (e.g., non-existent or corrupted), or
	// the saved node ID is different than the current node ID, generate
	// a random clock sequence value.
	if (o.Saver == nil || !bytes.Equal(storage.Node, node)) {

		// 4.1.5.  Clock Sequence https://www.ietf.org/rfc/rfc4122.txt
		//
		// For UUID version 1, the clock sequence is used to help avoid
		// duplicates that could arise when the clock is set backwards in time
		// or if the node ID changes.
		//
		// If the clock is set backwards, or might have been set backwards
		// (e.g., while the system was powered off), and the UUID generator can
		// not be sure that no UUIDs were generated with timestamps larger than
		// the value to which the clock was set, then the clock sequence has to
		// be changed.  If the previous value of the clock sequence is known, it
		// can just be incremented; otherwise it should be set to a random or
		// high-quality pseudo-random value.

		// The clock sequence MUST be originally (i.e., once in the lifetime of
		// a system) initialized to a random number to minimize the correlation
		// across systems.  This provides maximum protection against node
		// identifiers that may move or switch from system to system rapidly.
		// The initial value MUST NOT be correlated to the node identifier.
		err := binary.Read(rand.Reader, binary.LittleEndian, storage.Sequence)
		if err != nil {
			log.Println("uuid.State.init error:", err)
		} else {
			log.Printf("uuid.State.init initialised random sequence: [%d]", storage.Sequence)
		}

		// If the state was available, but the saved timestamp is later than
		// the current timestamp, increment the clock sequence value.

	} else if (now < storage.Timestamp) {
		storage.Sequence++;
	}

	o.Store = &storage
}

func (o *generator) save() {
	if (o.Saver != nil) {
		go func(pState *generator) {
			pState.Lock()
			defer pState.Unlock()
			pState.Save(pState.Store)
		}(o)
	}
}

func getHardwareAddress() (node net.HardwareAddr) {
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, i := range interfaces {
			// Initially I could multicast out the Flags to get
			// whether the interface was up but started failing
			if (i.Flags & (1 << net.FlagUp)) != 0 {
				//if inter.Flags.String() != "0" {
				if addrs, err := i.Addrs(); err == nil {
					for _, a := range addrs {
						if a.String() != "0.0.0.0" && !bytes.Equal(i.HardwareAddr, make([]byte, len(i.HardwareAddr))) {
							// Don't use random as we have a real address
							node = i.HardwareAddr
							return
						}
					}
				}
			}
		}
	}
	log.Println("uuid.State.init: address error: will generate random node id instead", err)

	node = make([]byte, 6)

	if _, err := rand.Read(node); err != nil {
		log.Panicln("uuid.getHardwareAddress: could not get cryto random bytes", err)
	}
	// Mark as randomly generated
	node[0] |= 0x01
	return
}
