package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	log "github.com/sirupsen/logrus"
)

// reconnectCooldown is the minimum time to wait after a device connection closes
// before opening a new one to the same port. This prevents rapid reconnection cycles
// from overwhelming the device's lockdown service (which returns error code 3
// when it can't keep up with connection churn).
const reconnectCooldown = 500 * time.Millisecond

type iosproxy struct {
	tcpConn    net.Conn
	deviceConn ios.DeviceConnectionInterface
}

type ConnListener struct {
	listener      net.Listener
	quit          chan interface{}
	lastCloseTime time.Time
	mu            sync.Mutex
}

// Forward forwards every connection made to the hostPort to whatever service runs inside an app on the device on phonePort.
// Port values must be between 1 and 65535.
func Forward(device ios.DeviceEntry, hostPort uint16, phonePort uint16) (*ConnListener, error) {
	if hostPort == 0 {
		return nil, fmt.Errorf("forward: invalid host port: port must be at least 1")
	}
	if phonePort == 0 {
		return nil, fmt.Errorf("forward: invalid target port: port must be at least 1")
	}
	log.Infof("Start listening on port %d forwarding to port %d on device", hostPort, phonePort)
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", hostPort))
	if err != nil {
		return nil, fmt.Errorf("forward: failed listener with err: %w", err)
	}
	cl := &ConnListener{
		listener: l,
		quit:     make(chan interface{}),
	}

	go connectionAccept(cl, device.DeviceID, phonePort)

	return cl, nil
}

// Close stops listening on the host port for the forwarded connection
func (cl *ConnListener) Close() error {
	close(cl.quit)

	err := cl.listener.Close()
	if err != nil {
		return fmt.Errorf("forward: failed closing listener with err: %w", err)
	}

	return nil
}

// recordClose records the time a device connection was closed, used for cooldown enforcement.
func (cl *ConnListener) recordClose() {
	cl.mu.Lock()
	cl.lastCloseTime = time.Now()
	cl.mu.Unlock()
}

// waitForCooldown sleeps if necessary to enforce the minimum reconnect interval.
func (cl *ConnListener) waitForCooldown() {
	cl.mu.Lock()
	last := cl.lastCloseTime
	cl.mu.Unlock()

	if last.IsZero() {
		return
	}
	elapsed := time.Since(last)
	if elapsed < reconnectCooldown {
		wait := reconnectCooldown - elapsed
		log.Debugf("forward: waiting %v before reconnecting to device port (cooldown)", wait)
		time.Sleep(wait)
	}
}

func connectionAccept(cl *ConnListener, deviceID int, phonePort uint16) {
	for {
		select {
		case <-cl.quit:
			log.WithFields(log.Fields{"phonePort": phonePort}).Info("closed listener successfully")
			return
		default:
			clientConn, err := cl.listener.Accept()
			if err != nil {
				log.Errorf("Error accepting new connection %v", err)
				continue
			}
			log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", cl)}).Info("new client connected")
			go func() {
				cl.waitForCooldown()
				StartNewProxyConnection(context.TODO(), clientConn, deviceID, phonePort, cl)
			}()
		}
	}
}

func StartNewProxyConnection(ctx context.Context, clientConn io.ReadWriteCloser, deviceID int, phonePort uint16, cl *ConnListener) error {
	usbmuxConn, err := ios.NewUsbMuxConnectionSimple()
	if err != nil {
		log.Errorf("could not connect to usbmuxd: %+v", err)
		clientConn.Close()
		return fmt.Errorf("could not connect to usbmuxd: %v", err)
	}
	muxError := usbmuxConn.Connect(deviceID, phonePort)
	if muxError != nil {
		log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn), "err": muxError, "phonePort": phonePort}).Infof("could not connect to phone")
		clientConn.Close()
		if cl != nil {
			cl.recordClose()
		}
		return fmt.Errorf("could not connect to port:%d on iOS: %v", phonePort, err)
	}
	log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn), "phonePort": phonePort}).Infof("Connected to port")
	deviceConn := usbmuxConn.ReleaseDeviceConnection()

	// proxyConn := iosproxy{clientConn, deviceConn}
	ctx2, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)

	closed := false
	go func() {
		io.Copy(clientConn, deviceConn.Reader())
		if ctx2.Err() == nil {
			cancel()
			clientConn.Close()
			deviceConn.Close()
			closed = true
		}

		log.Errorf("forward: close clientConn <-- deviceConn")
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		io.Copy(deviceConn.Writer(), clientConn)
		if ctx2.Err() == nil {
			cancel()
			clientConn.Close()
			deviceConn.Close()
			closed = true
		}

		log.Errorf("forward: close clientConn --> deviceConn")
		wg.Done()
	}()

	<-ctx2.Done()
	if !closed {
		clientConn.Close()
		deviceConn.Close()
	}

	wg.Wait()
	if cl != nil {
		cl.recordClose()
	}
	return nil
}

func (proxyConn *iosproxy) Close() {
	proxyConn.tcpConn.Close()
}
