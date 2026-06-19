package testgovpp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
)

type Manager struct {
	log  zerolog.Logger
	mu   sync.Mutex
	tgs  map[models.TargetGroupID]*targetGroup
	seed maphash.Seed
}

type targetGroup struct {
	id   models.TargetGroupID
	spec models.TargetGroupSpec

	endpoints map[string]models.EndpointSpec

	tcpLn net.Listener

	udpPC      net.PacketConn
	udpDone    chan struct{}
	udpMu      sync.Mutex
	udpSession map[udpKey]*udpSess
}

type udpKey struct {
	srcIP   [16]byte
	dstIP   [16]byte
	srcPort uint16
	dstPort uint16
	proto   uint8 // 6 TCP, 17 UDP
}

type udpSess struct {
	backend     models.EndpointSpec
	last        time.Time
	backendConn *net.UDPConn
	clientAddr  *net.UDPAddr
}

func New(log zerolog.Logger) *Manager {
	return &Manager{
		log:  log,
		tgs:  make(map[models.TargetGroupID]*targetGroup),
		seed: maphash.MakeSeed(),
	}
}

func (m *Manager) ApplySpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Replace if exists (idempotent-ish)
	if _, ok := m.tgs[tgID]; ok {
		_ = m.removeLocked(tgID)
	}

	tg := &targetGroup{
		id:        tgID,
		spec:      spec,
		endpoints: make(map[string]models.EndpointSpec),

		udpDone:    make(chan struct{}),
		udpSession: make(map[udpKey]*udpSess),
	}

	port := int(spec.Port)
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port %d", spec.Port)
	}

	switch spec.Protocol {
	case models.TCP:
		if err := m.startTCP(tg); err != nil {
			return err
		}
	case models.UDP:
		if err := m.startUDP(tg); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported protocol %q", spec.Protocol)
	}

	m.tgs[tgID] = tg

	m.log.Info().
		Str("tg", string(tgID)).
		Str("proto", string(spec.Protocol)).
		Uint32("port", spec.Port).
		Str("vip", spec.VirtualIP.String()).
		Msg("userspace dataplane applied spec")

	return nil
}

func (m *Manager) RemoveSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeLocked(tgID)
}

func (m *Manager) removeLocked(tgID models.TargetGroupID) error {
	tg, ok := m.tgs[tgID]
	if !ok {
		return nil
	}

	if tg.tcpLn != nil {
		_ = tg.tcpLn.Close()
		tg.tcpLn = nil
	}

	if tg.udpPC != nil {
		close(tg.udpDone)
		_ = tg.udpPC.Close()
		tg.udpPC = nil
	}

	delete(m.tgs, tgID)

	m.log.Info().Str("tg", string(tgID)).Msg("userspace dataplane removed spec")
	return nil
}

func (m *Manager) AddEndpoints(ctx context.Context, tgID models.TargetGroupID, desired []models.EndpointSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tg, ok := m.tgs[tgID]
	if !ok {
		return fmt.Errorf("spec for tg %s not found", tgID)
	}

	for _, ep := range desired {
		if ep.Weight == 0 {
			continue
		}
		key := joinHostPort(ep.IP, ep.Port)
		tg.endpoints[key] = ep
	}

	return nil
}

func (m *Manager) RemoveEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tg, ok := m.tgs[tgID]
	if !ok {
		return nil
	}

	for _, ep := range endpoints {
		key := joinHostPort(ep.IP, ep.Port)
		delete(tg.endpoints, key)
	}
	return nil
}

func (m *Manager) startTCP(tg *targetGroup) error {
	addr := ":" + strconv.Itoa(int(tg.spec.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp listen %s: %w", addr, err)
	}
	tg.tcpLn = ln

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				m.log.Warn().Err(err).Str("tg", string(tg.id)).Msg("accept failed")
				continue
			}
			go m.handleTCPConn(tg, c)
		}
	}()

	return nil
}

func (m *Manager) handleTCPConn(tg *targetGroup, c net.Conn) {
	defer c.Close()

	src := c.RemoteAddr().(*net.TCPAddr)
	dst := c.LocalAddr().(*net.TCPAddr)

	// Snapshot endpoints
	m.mu.Lock()
	eps := make([]models.EndpointSpec, 0, len(tg.endpoints))
	for _, ep := range tg.endpoints {
		eps = append(eps, ep)
	}
	m.mu.Unlock()

	if len(eps) == 0 {
		_ = c.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = io.WriteString(c, "no backends\n")
		return
	}

	backend, ok := m.pickHRW5Tuple(
		6,
		src.IP, uint16(src.Port),
		dst.IP, uint16(dst.Port),
		eps,
	)
	if !ok {
		return
	}

	up, err := net.DialTimeout("tcp", joinHostPort(backend.IP, backend.Port), 2*time.Second)
	if err != nil {
		m.log.Warn().Err(err).
			Str("tg", string(tg.id)).
			Str("backend", joinHostPort(backend.IP, backend.Port)).
			Msg("dial backend failed")
		return
	}
	defer up.Close()

	_ = c.SetDeadline(time.Time{})
	_ = up.SetDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
	<-done
}

func (m *Manager) startUDP(tg *targetGroup) error {
	addr := ":" + strconv.Itoa(int(tg.spec.Port))
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("udp listen %s: %w", addr, err)
	}
	tg.udpPC = pc

	go m.udpLoop(tg)
	go m.udpReaper(tg, 30*time.Second, 60*time.Second)

	return nil
}

func (m *Manager) udpLoop(tg *targetGroup) {
	buf := make([]byte, 64*1024)

	for {
		select {
		case <-tg.udpDone:
			return
		default:
		}

		n, clientAddr, err := tg.udpPC.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			m.log.Warn().Err(err).Str("tg", string(tg.id)).Msg("udp read failed")
			continue
		}

		ca, ok := clientAddr.(*net.UDPAddr)
		if !ok {
			continue
		}

		// dstIP: best effort:
		// - if VirtualIP in spec is set, use it (so hashing is stable and "VIP-like")
		// - else 0.0.0.0
		dstIP := tg.spec.VirtualIP
		if len(dstIP) == 0 {
			dstIP = net.IPv4zero
		}

		key := make5TupleKey(17, ca.IP, uint16(ca.Port), dstIP, uint16(tg.spec.Port))
		backend, sess := m.getOrCreateUDPSess(tg, key, ca)
		if sess == nil {
			continue
		}

		_, err = sess.backendConn.Write(buf[:n])
		if err != nil {
			m.log.Warn().Err(err).
				Str("tg", string(tg.id)).
				Str("backend", joinHostPort(backend.IP, backend.Port)).
				Msg("udp write to backend failed")
		}
	}
}

func (m *Manager) getOrCreateUDPSess(tg *targetGroup, key udpKey, client *net.UDPAddr) (models.EndpointSpec, *udpSess) {
	// existing?
	tg.udpMu.Lock()
	if s, ok := tg.udpSession[key]; ok {
		s.last = time.Now()
		tg.udpMu.Unlock()
		return s.backend, s
	}
	tg.udpMu.Unlock()

	// snapshot endpoints
	m.mu.Lock()
	eps := make([]models.EndpointSpec, 0, len(tg.endpoints))
	for _, ep := range tg.endpoints {
		eps = append(eps, ep)
	}
	m.mu.Unlock()

	if len(eps) == 0 {
		return models.EndpointSpec{}, nil
	}

	backend, ok := m.pickHRWFromKey(key, eps)
	if !ok {
		return models.EndpointSpec{}, nil
	}

	raddr, err := net.ResolveUDPAddr("udp", joinHostPort(backend.IP, backend.Port))
	if err != nil {
		return models.EndpointSpec{}, nil
	}

	bc, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return models.EndpointSpec{}, nil
	}

	sess := &udpSess{
		backend:     backend,
		last:        time.Now(),
		backendConn: bc,
		clientAddr:  client,
	}

	go m.udpBackendToClientLoop(tg, sess)

	tg.udpMu.Lock()
	if old, exists := tg.udpSession[key]; exists {
		tg.udpMu.Unlock()
		_ = bc.Close()
		return old.backend, old
	}
	tg.udpSession[key] = sess
	tg.udpMu.Unlock()

	return backend, sess
}

func (m *Manager) udpBackendToClientLoop(tg *targetGroup, sess *udpSess) {
	defer sess.backendConn.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := sess.backendConn.Read(buf)
		if err != nil {
			return
		}
		_, _ = tg.udpPC.WriteTo(buf[:n], sess.clientAddr)
	}
}

func (m *Manager) udpReaper(tg *targetGroup, interval, ttl time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-tg.udpDone:
			return
		case <-t.C:
			now := time.Now()
			tg.udpMu.Lock()
			for k, s := range tg.udpSession {
				if now.Sub(s.last) > ttl {
					_ = s.backendConn.Close()
					delete(tg.udpSession, k)
				}
			}
			tg.udpMu.Unlock()
		}
	}
}

// ---- hashing (weighted rendezvous) ----

func (m *Manager) pickHRW5Tuple(proto uint8, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, eps []models.EndpointSpec) (models.EndpointSpec, bool) {
	key := make5TupleKey(proto, srcIP, srcPort, dstIP, dstPort)
	return m.pickHRWFromKey(key, eps)
}

func (m *Manager) pickHRWFromKey(key udpKey, eps []models.EndpointSpec) (models.EndpointSpec, bool) {
	var (
		best      models.EndpointSpec
		bestScore uint64
		h         maphash.Hash
	)
	h.SetSeed(m.seed)

	for _, ep := range eps {
		if ep.Weight == 0 {
			continue
		}

		h.Reset()
		_, _ = h.Write(key.srcIP[:])
		_, _ = h.Write(key.dstIP[:])

		var b [2]byte
		binary.BigEndian.PutUint16(b[:], key.srcPort)
		_, _ = h.Write(b[:])
		binary.BigEndian.PutUint16(b[:], key.dstPort)
		_, _ = h.Write(b[:])

		_, _ = h.Write([]byte{key.proto})

		// backend identity
		_, _ = h.Write(ep.IP.To16())
		var p [2]byte
		binary.BigEndian.PutUint16(p[:], ep.Port)
		_, _ = h.Write(p[:])

		score := h.Sum64()
		// weight adjustment: larger weight => better chance to win
		score = score / uint64(ep.Weight)

		if bestScore == 0 || score > bestScore {
			bestScore = score
			best = ep
		}
	}

	if bestScore == 0 {
		return models.EndpointSpec{}, false
	}
	return best, true
}

func make5TupleKey(proto uint8, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16) udpKey {
	var k udpKey
	copy16(&k.srcIP, srcIP)
	copy16(&k.dstIP, dstIP)
	k.srcPort = srcPort
	k.dstPort = dstPort
	k.proto = proto
	return k
}

func copy16(dst *[16]byte, ip net.IP) {
	ip16 := ip.To16()
	if ip16 == nil {
		*dst = [16]byte{}
		return
	}
	copy(dst[:], ip16)
}

func joinHostPort(ip net.IP, port uint16) string {
	return net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
}
