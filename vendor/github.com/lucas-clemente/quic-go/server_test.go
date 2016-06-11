package quic

import (
	"net"

	"github.com/lucas-clemente/quic-go/crypto"
	"github.com/lucas-clemente/quic-go/handshake"
	"github.com/lucas-clemente/quic-go/protocol"
	"github.com/lucas-clemente/quic-go/testdata"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type mockSession struct {
	connectionID protocol.ConnectionID
	packetCount  int
	closed       bool
}

func (s *mockSession) handlePacket(addr interface{}, hdr *publicHeader, data []byte) {
	s.packetCount++
}

func (s *mockSession) run()              {}
func (s *mockSession) Close(error) error { s.closed = true; return nil }

func newMockSession(conn connection, v protocol.VersionNumber, connectionID protocol.ConnectionID, sCfg *handshake.ServerConfig, streamCallback StreamCallback, closeCallback closeCallback) (packetHandler, error) {
	return &mockSession{
		connectionID: connectionID,
	}, nil
}

var _ = Describe("Server", func() {
	Describe("with mock session", func() {
		var (
			server *Server
		)

		BeforeEach(func() {
			server = &Server{
				sessions:   map[protocol.ConnectionID]packetHandler{},
				newSession: newMockSession,
			}
		})

		It("composes version negotiation packets", func() {
			expected := append(
				[]byte{0x01 | 0x08 | 0x04, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
				protocol.SupportedVersionsAsTags...,
			)
			Expect(composeVersionNegotiation(1)).To(Equal(expected))
		})

		It("creates new sessions", func() {
			err := server.handlePacket(nil, nil, []byte{0x08, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c, 0x01})
			Expect(err).ToNot(HaveOccurred())
			Expect(server.sessions).To(HaveLen(1))
			Expect(server.sessions[0x4cfa9f9b668619f6].(*mockSession).connectionID).To(Equal(protocol.ConnectionID(0x4cfa9f9b668619f6)))
			Expect(server.sessions[0x4cfa9f9b668619f6].(*mockSession).packetCount).To(Equal(1))
		})

		It("assigns packets to existing sessions", func() {
			err := server.handlePacket(nil, nil, []byte{0x08, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c, 0x01})
			Expect(err).ToNot(HaveOccurred())
			err = server.handlePacket(nil, nil, []byte{0x08, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c, 0x01})
			Expect(err).ToNot(HaveOccurred())
			Expect(server.sessions).To(HaveLen(1))
			Expect(server.sessions[0x4cfa9f9b668619f6].(*mockSession).connectionID).To(Equal(protocol.ConnectionID(0x4cfa9f9b668619f6)))
			Expect(server.sessions[0x4cfa9f9b668619f6].(*mockSession).packetCount).To(Equal(2))
		})

		It("closes and deletes sessions", func() {
			pheader := []byte{0x09, 0xf6, 0x19, 0x86, 0x66, 0x9b, 0x9f, 0xfa, 0x4c, 0x51, 0x30, 0x33, 0x32, 0x01}
			err := server.handlePacket(nil, nil, append(pheader, (&crypto.NullAEAD{}).Seal(0, pheader, nil)...))
			Expect(err).ToNot(HaveOccurred())
			Expect(server.sessions).To(HaveLen(1))
			server.closeCallback(0x4cfa9f9b668619f6)
			// The server should now have closed the session, leaving a nil value in the sessions map
			Expect(server.sessions).To(HaveLen(1))
			Expect(server.sessions[0x4cfa9f9b668619f6]).To(BeNil())
		})

		It("closes sessions when Close is called", func() {
			session := &mockSession{}
			server.sessions[1] = session
			err := server.Close()
			Expect(err).NotTo(HaveOccurred())
			Expect(session.closed).To(BeTrue())
		})
	})

	It("setups and responds with version negotiation", func(done Done) {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())

		server, err := NewServer("", testdata.GetTLSConfig(), nil)
		Expect(err).ToNot(HaveOccurred())

		serverConn, err := net.ListenUDP("udp", addr)
		Expect(err).NotTo(HaveOccurred())

		addr = serverConn.LocalAddr().(*net.UDPAddr)

		go func() {
			defer GinkgoRecover()
			err := server.Serve(serverConn)
			Expect(err).ToNot(HaveOccurred())
			close(done)
		}()

		clientConn, err := net.DialUDP("udp", nil, addr)
		Expect(err).ToNot(HaveOccurred())

		_, err = clientConn.Write([]byte{0x09, 0x01, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x01, 'Q', '0', '0', '0', 0x01})
		Expect(err).NotTo(HaveOccurred())
		data := make([]byte, 1000)
		var n int
		n, _, err = clientConn.ReadFromUDP(data)
		Expect(err).NotTo(HaveOccurred())
		data = data[:n]
		expected := append(
			[]byte{0xd, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
			protocol.SupportedVersionsAsTags...,
		)
		Expect(data).To(Equal(expected))

		err = server.Close()
		Expect(err).ToNot(HaveOccurred())
	})

	It("setups and responds with error on invalid frame", func(done Done) {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())

		server, err := NewServer("", testdata.GetTLSConfig(), nil)
		Expect(err).ToNot(HaveOccurred())

		serverConn, err := net.ListenUDP("udp", addr)
		Expect(err).NotTo(HaveOccurred())

		addr = serverConn.LocalAddr().(*net.UDPAddr)

		go func() {
			defer GinkgoRecover()
			err := server.Serve(serverConn)
			Expect(err).ToNot(HaveOccurred())
			close(done)
		}()

		clientConn, err := net.DialUDP("udp", nil, addr)
		Expect(err).ToNot(HaveOccurred())

		_, err = clientConn.Write([]byte{0x09, 0x01, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x01, 'Q', '0', '0', '0', 0x01, 0x00})
		Expect(err).NotTo(HaveOccurred())
		data := make([]byte, 1000)
		var n int
		n, _, err = clientConn.ReadFromUDP(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).ToNot(BeZero())

		err = server.Close()
		Expect(err).ToNot(HaveOccurred())
	})
})