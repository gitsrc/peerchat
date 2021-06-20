package src

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	discovery "github.com/libp2p/go-libp2p-discovery"
	host "github.com/libp2p/go-libp2p-host"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/mr-tron/base58/base58"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	"github.com/sirupsen/logrus"
)

const service = "manishmeganathan/peerchat"

// A structure that represents a P2P Host
type P2P struct {
	// Represents the host context layer
	Ctx context.Context

	// Represents the libp2p host
	Host host.Host

	// Represents the DHT routing table
	KadDHT *dht.IpfsDHT

	// Represents the peer discovery service
	Discovery *discovery.RoutingDiscovery

	// Represents the PubSub Handler
	PubSub *pubsub.PubSub
}

/*
A constructor function that generates and returns a P2P object.

Constructs a libp2p host with TLS encrypted secure transportation that works over a TCP
transport connection using a Yamux Stream Multiplexer and uses UPnP for the NAT traversal.

A Kademlia DHT is then bootstrapped on this host using the default peers offered by libp2p
and a Peer Discovery service is created from this Kademlia DHT. The PubSub handler is then
created on the host using the peer discovery service created prior.
*/
func NewP2P() *P2P {
	// Setup a background context
	ctx := context.Background()

	// Setup a P2P Host Node
	nodehost := setupHost(ctx)
	// Info log
	logrus.Infoln("Created the P2P Host.")

	// Create and Setup a Kademlia DHT on the host
	kaddht := setupKadDHT(ctx, nodehost)
	// Info log
	logrus.Infoln("Created the Kademlia DHT on the P2P Host.")

	// Bootstrap the Kad DHT
	bootstrapDHT(ctx, nodehost, kaddht)
	// Info log
	logrus.Infoln("Bootstrapped the Kademlia DHT and Connected to Bootstrap Peers")

	// Create a peer discovery service using the Kad DHT
	routingdiscovery := discovery.NewRoutingDiscovery(kaddht)
	// Info log
	logrus.Infoln("Created the Peer Discovery Service.")

	// Create a PubSub handler with the routing discovery
	pubsubhandler := setupPubSub(ctx, nodehost, routingdiscovery)
	// Debug log
	logrus.Infoln("Created the PubSub Handler.")

	// Return the P2P object
	return &P2P{
		Ctx:       ctx,
		Host:      nodehost,
		KadDHT:    kaddht,
		Discovery: routingdiscovery,
		PubSub:    pubsubhandler,
	}
}

// A method of P2P to connect to service peers.
// This method uses the Advertise() functionality of the Peer Discovery Service
// to advertise the service and then disovers all peers advertising the same.
// The peer discovery is handled by a go-routine that will read from a channel
// of peer address information until the peer channel closes
func (p2p *P2P) AdvertiseConnect() {
	// Advertise the availabilty of the service on this node
	ttl, err := p2p.Discovery.Advertise(p2p.Ctx, service)
	// Info log
	logrus.Infoln("Advertised the PeerChat Service.")
	// Sleep to give time for the advertisment to propogate
	time.Sleep(time.Second * 5)
	// Debug log
	logrus.Debugln("Service Time-to-Live is %s", ttl)

	// Find all peers advertising the same service
	peerchan, err := p2p.Discovery.FindPeers(p2p.Ctx, service)
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("P2P Peer Discovery Failed!")
	}
	// Debug log
	logrus.Debugln("Discovered PeerChat Service Peers.")

	// Connect to peers as they are discovered
	go handlePeerDiscovery(p2p.Host, peerchan)
	// Debug log
	logrus.Debugln("Started Peer Connection Handler.")
}

// A method of P2P to connect to service peers.
// This method uses the Provide() functionality of the Kademlia DHT directly to announce
// the ability to provide the service and then disovers all peers that provide the same.
// The peer discovery is handled by a go-routine that will read from a channel
// of peer address information until the peer channel closes
func (p2p *P2P) AnnounceConnect() {
	// Generate the Service CID
	cidvalue := generateCID(service)
	// Debug log
	logrus.Debugln("Generated the Service CID.")

	// Announce that this host can provide the service CID
	err := p2p.KadDHT.Provide(p2p.Ctx, cidvalue, true)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Announce Service CID!")
	}
	// Info log
	logrus.Infoln("Announced the PeerChat Service.")
	// Sleep to give time for the advertisment to propogate
	time.Sleep(time.Second * 5)

	// Find the other providers for the service CID
	peerchan := p2p.KadDHT.FindProvidersAsync(p2p.Ctx, cidvalue, 0)
	// Debug log
	logrus.Debugln("Discovered PeerChat Service Peers.")

	// Connect to peers as they are discovered
	go handlePeerDiscovery(p2p.Host, peerchan)
	// Info log
	logrus.Infoln("Started Peer Connection Handler.")
}

// A function that generates the p2p configuration options and creates a
// libp2p host object for the given context. The created host is returned
func setupHost(ctx context.Context) host.Host {
	// Set up the host identity options
	prvkey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	//identity := libp2p.Identity(prvkey)
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Generate P2P Identity Configuration!", prvkey)
	}

	// Debug log
	logrus.Debugln("Generated P2P Identity Configurations.")

	// Set up TLS secured transport options
	//tlstransport, err := tls.New(prvkey)
	//security := libp2p.Security(tls.ID, tlstransport)
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Generate P2P Secure Transport Configuration!")
	}

	// Debug log
	logrus.Debugln("Generated P2P Security Configurations.")

	// Set up host listener address options
	sourcemultiaddr, err := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/0")
	listen := libp2p.ListenAddrs(sourcemultiaddr)
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Generate P2P Address Listener Configuration!")
	}

	// Debug log
	logrus.Debugln("Generated P2P Address Listener Configuration.")

	// Set up the transport, stream mux and NAT options
	//transport := libp2p.Transport(tcp.NewTCPTransport)
	//muxer := libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport)
	nat := libp2p.NATPortMap()

	// Debug log
	logrus.Debugln("Generated P2P Transport, Stream Multiplexer and NAT Configurations.")

	// Construct a new libP2P host with the created options
	libhost, err := libp2p.New(
		ctx,
		listen,
		//security,
		//transport,
		//muxer,
		//identity,
		nat,
	)
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Create the P2P Host!")
	}

	// Return the created host
	return libhost
}

// A function that generates a Kademlia DHT object and returns it
func setupKadDHT(ctx context.Context, nodehost host.Host) *dht.IpfsDHT {
	// Create DHT server mode option
	dhtmode := dht.Mode(dht.ModeServer)
	// Rertieve the list of boostrap peer addresses
	bootstrappeers := dht.GetDefaultBootstrapPeerAddrInfos()
	// Create the DHT bootstrap peers option
	dhtpeers := dht.BootstrapPeers(bootstrappeers...)

	// Debug log
	logrus.Debugln("Generated DHT Configuration.")

	// Start a Kademlia DHT on the host in server mode
	kaddht, err := dht.New(ctx, nodehost, dhtmode, dhtpeers)
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Create the Kademlia DHT!")
	}

	// Return the KadDHT
	return kaddht
}

// A function that generates a PubSub Handler object and returns it
// Requires a node host and a routing discovery service.
func setupPubSub(ctx context.Context, nodehost host.Host, routingdiscovery *discovery.RoutingDiscovery) *pubsub.PubSub {
	// Create a new PubSub service which uses a GossipSub router
	pubsubhandler, err := pubsub.NewGossipSub(ctx, nodehost, pubsub.WithDiscovery(routingdiscovery))
	// Handle any potential error
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
			"type":  "GossipSub",
		}).Fatalln("PubSub Handler Creation Failed!")
	}

	// Return the PubSub handler
	return pubsubhandler
}

// A function that bootstraps a given Kademlia DHT to satisfy the IPFS router
// interface and connects to all the bootstrap peers provided by libp2p
func bootstrapDHT(ctx context.Context, nodehost host.Host, kaddht *dht.IpfsDHT) {
	// Bootstrap the DHT to satisfy the IPFS Router interface
	if err := kaddht.Bootstrap(ctx); err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Bootstrap the Kademlia!")
	}

	// Debug log
	logrus.Debugln("Set the Kademlia DHT into Bootstrap Mode.")

	// Declare a WaitGroup
	var wg sync.WaitGroup
	// Declare counters for the number of bootstrap peers
	var connectedbootpeers int
	var totalbootpeers int

	// Iterate over the default bootstrap peers provided by libp2p
	for _, peeraddr := range dht.DefaultBootstrapPeers {
		// Retrieve the peer address information
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peeraddr)

		// Incremenent waitgroup counter
		wg.Add(1)
		// Start a goroutine to connect to each bootstrap peer
		go func() {
			// Defer the waitgroup decrement
			defer wg.Done()
			// Attempt to connect to the bootstrap peer
			if err := nodehost.Connect(ctx, *peerinfo); err != nil {
				// Increment the total bootstrap peer count
				totalbootpeers++
			} else {
				// Increment the connected bootstrap peer count
				connectedbootpeers++
				// Increment the total bootstrap peer count
				totalbootpeers++
			}
		}()
	}

	// Wait for the waitgroup to complete
	wg.Wait()

	// Log the number of bootstrap peers connected
	logrus.Debugf("Connected to %d out of %d Bootstrap Peers.", connectedbootpeers, totalbootpeers)
}

// A function that connects the given host to all peers recieved from a
// channel of peer address information. Meant to be started as a go routine.
func handlePeerDiscovery(nodehost host.Host, peerchan <-chan peer.AddrInfo) {
	// Iterate over the peer channel
	for peer := range peerchan {
		// Ignore if the discovered peer is the host itself
		if peer.ID == nodehost.ID() {
			continue
		}

		// Connect to the peer
		nodehost.Connect(context.Background(), peer)
	}
}

// A function that generates a CID object for a given string and returns it.
// Uses SHA256 to hash the string and generate a multihash from it.
// The mulithash is then base58 encoded and then used to create the CID
func generateCID(namestring string) cid.Cid {
	// Hash the service content ID with SHA256
	hash := sha256.Sum256([]byte(namestring))
	// Append the hash with the hashing codec ID for SHA2-256 (0x12),
	// the digest size (0x20) and the hash of the service content ID
	finalhash := append([]byte{0x12, 0x20}, hash[:]...)
	// Encode the fullhash to Base58
	b58string := base58.Encode(finalhash)

	// Generate a Multihash from the base58 string
	mulhash, err := multihash.FromB58String(string(b58string))
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Fatalln("Failed to Generate Service CID!")
	}

	// Generate a CID from the Multihash
	cidvalue := cid.NewCidV1(12, mulhash)
	// Return the CID
	return cidvalue
}
