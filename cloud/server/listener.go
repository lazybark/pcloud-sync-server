package server

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"time"

	"github.com/lazybark/pcloud-sync-server/cloud/events"
	"github.com/lazybark/pcloud-sync-server/cloud/proto"
)

func (s *Server) GetTLSCOnfig() (tlsConfig *tls.Config, err error) {
	cert, err := tls.LoadX509KeyPair(s.config.CertPath, s.config.KeyPath)
	if err != nil {
		return tlsConfig, fmt.Errorf("[GetTLSCOnfig] error getting key pair -> %w", err)
	}
	tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

	return tlsConfig, nil
}

func (s *Server) Listen() {
	tlsConfig, err := s.GetTLSCOnfig()
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceSyncServer.String(), fmt.Errorf("[Listen] error getting key pair -> %w", err))
	}

	s.evProc.Send(EventType("cyan"), events.SourceSyncServer.String(), fmt.Sprintf("Listening on %s:%s", s.config.HostName, s.config.Port))

	l, err := tls.Listen("tcp", ":"+s.config.Port, tlsConfig)
	if err != nil {
		s.evProc.Send(EventType("fatal"), events.SourceSyncServer.String(), fmt.Errorf("[Listen] error listening -> %w", err))
	}
	defer func() {
		err := l.Close()
		if err != nil {
			s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("error closing connection -> %w", err))
		}
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v)[Listen] error accepting connection: %w", conn.RemoteAddr(), err))
		}
		s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[New Connection] Active: %d", conn.RemoteAddr(), s.activeConnectionsNumber+1))

		go func(c net.Conn) {
			eventsChannel := make(chan FSEventNotification)
			connStateChannel := make(chan ConnectionEvent)
			connection := ActiveConnection{
				EventsChan: eventsChannel,
				IP:         c.RemoteAddr(),
				ConnectAt:  time.Now(),
				StateChan:  connStateChannel,
				Active:     true,
			}
			s.connectionPoolMutex.Lock()
			connection.NumerInPool = len(s.activeConnections)
			s.activeConnections = append(s.activeConnections, connection)
			s.activeConnectionsNumber++
			s.connectionPoolMutex.Unlock()

			var message proto.Message
			var response proto.Message
			var recievedBytes uint

			response.Token = s.config.ServerToken
			//response.AppVersion = s.AppVersion
			//response.PartyName = s.Config.ServerName
			//var bytesSent int

			// Routine to maintain current connection
			go func() {
				for {
					select {
					case data, ok := <-eventsChannel:
						if !ok {
							io.WriteString(c, "Channel closed")
							// Close sync in pool
							s.connectionPoolMutex.Lock()
							s.activeConnections[connection.NumerInPool].SyncActive = false
							s.connectionPoolMutex.Unlock()
							return
						}
						fmt.Println("Sending to client")
						_, err := message.SendSyncEvent(c, data.Event.Op, data.Object)
						if err != nil {
							fmt.Println(err)
						}
						/*_, err := io.WriteString(c, fmt.Sprintf("%s %s", s.FW.EscapeAddress(event.Name), event.Op)+MessageTerminatorString)
						if err != nil {
							fmt.Println(err)
						}*/

					case state, ok := <-connStateChannel:
						if !ok || state == ConnectionClose {
							io.WriteString(c, "Connection closed")

							fmt.Println("Connection closing")
							// Close sync in pool
							return
						} else if state == ConnectionSyncStart {
							fmt.Println("Sync started")
							s.connectionPoolMutex.Lock()
							s.activeConnections[connection.NumerInPool].SyncActive = true
							s.connectionPoolMutex.Unlock()
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) sync started", conn.RemoteAddr()))

						} else if state == ConnectionSyncStop {
							fmt.Println("Sync stopped")
							s.connectionPoolMutex.Lock()
							s.activeConnections[connection.NumerInPool].SyncActive = false
							s.connectionPoolMutex.Unlock()
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) sync stopped", conn.RemoteAddr()))
						}
					}
				}
			}()

			for {
				netData, err := bufio.NewReader(c).ReadBytes(proto.MessageTerminator)
				if err != nil {

					// If connection closed - break the cycle
					if errors.As(err, &io.ErrClosedPipe) {
						s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) - conn closed by other party", conn.RemoteAddr()))
						s.CloseConnection(&connection, connStateChannel)
						break
					}

					continue
				}

				err = message.Parse(&netData)
				if err != nil {
					s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v)[message.Parse] - broken message: %v", conn.RemoteAddr(), err))
					if connection.ClientErrors >= s.config.MaxClientErrors {
						s.CloseConnection(&connection, connStateChannel)
					}
					continue
				}

				fmt.Println("client read:", string(netData))

				if message.Type == proto.MessageHello {
					hello, err := message.ParseHello()
					if err != nil {
						fmt.Println(err)
					}
					fmt.Println("Got hello from", hello)

					_, err = response.SendHandshake(conn, "test_client_1", s.appVersion, "lazybark.dev@gmail.com", 0, 15, 2048)
					if err != nil {
						s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
					}
					continue

				} else if message.Type == proto.MessageAuth {
					uid, newToken, err := message.ProcessFullAuth(&conn, s.db, s.config.TokenValidDays)
					if err != nil {
						s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[message.ProcessFullAuth] - error parsing auth: %v", conn.RemoteAddr(), err))

						bytesSent, err := response.ReturnError(&conn, proto.ErrInternal)
						if err != nil {
							s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
							s.CloseConnection(&connection, connStateChannel)
							continue
						}
						if s.config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.config.ServerVerboseLogging && !s.config.SilentMode {
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					if newToken == "" {
						s.evProc.Send(EventType("yellow"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[ReadBytes] wrong auth", conn.RemoteAddr()))

						bytesSent, err := response.ReturnError(&conn, proto.ErrAccessDenied)
						if err != nil {
							s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
							s.CloseConnection(&connection, connStateChannel)
							continue
						}
						if s.config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.config.ServerVerboseLogging && !s.config.SilentMode {
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					connection.Uid = uid
					s.connectionPoolMutex.Lock()
					s.activeConnections[connection.NumerInPool].Uid = uid
					s.connectionPoolMutex.Unlock()

					// Return token to client
					bytesSent, err := response.ReturnToken(&conn, newToken)
					if err != nil {
						s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
						s.CloseConnection(&connection, connStateChannel)
						continue
					}
					if s.config.CountStats {
						fmt.Println(bytesSent)
					}
					if s.config.ServerVerboseLogging && !s.config.SilentMode {
						s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
					}

					// Token persists in server memory only
					connection.Token = newToken
					continue
				} else if message.Type == proto.MessageOK {
					ok, err := message.ValidateOK()
					if err != nil {
						s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) error parsing OK: %s", conn.RemoteAddr(), err))

						_, err := response.ReturnError(&conn, proto.ErrBrokenMessage)
						if err != nil {
							s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
						}
						if connection.ClientErrors >= s.config.MaxClientErrors {
							s.CloseConnection(&connection, connStateChannel)
						}
						continue

					}
					if !ok.OK {
						s.evProc.Send(EventType("warn"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) client sent error: %s", conn.RemoteAddr(), ok.HumanReadable))
						continue
					}
					continue
				} else if message.Type == proto.MessageStartSync {

					ok := connection.Token == message.Token

					if !ok {
						s.evProc.Send(EventType("yellow"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) wrong token", conn.RemoteAddr()))

						bytesSent, err := response.ReturnError(&conn, proto.ErrAccessDenied)
						if err != nil {
							s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
							if connection.ServerErrors >= s.config.MaxServerErrors && s.config.MaxServerErrors > 0 {
								s.evProc.Send(EventType("warn"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
								s.CloseConnection(&connection, connStateChannel)
								return
							}
							continue
						}
						if s.config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.config.ServerVerboseLogging && !s.config.SilentMode {
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					// Starting sync channel with the other party
					connStateChannel <- ConnectionSyncStart
					// Requesting to start syncing back
					response.ReturnInfoMessage(&conn, proto.MessageOK)
					response.ReturnInfoMessage(&conn, proto.MessageStartSync)
					continue
				} else if message.Type == proto.MessageEndSync {

					ok := connection.Token == message.Token

					if !ok {
						s.evProc.Send(EventType("yellow"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) wrong token", conn.RemoteAddr()))

						bytesSent, err := response.ReturnError(&conn, proto.ErrAccessDenied)
						if err != nil {
							s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
							if connection.ServerErrors >= s.config.MaxServerErrors && s.config.MaxServerErrors > 0 {
								s.evProc.Send(EventType("warn"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
								s.CloseConnection(&connection, connStateChannel)
								return
							}
							continue
						}
						if s.config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.config.ServerVerboseLogging && !s.config.SilentMode {
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					// Starting sync channel with the other party
					connStateChannel <- ConnectionSyncStop
					// Requesting to start syncing back
					response.ReturnInfoMessage(&conn, proto.MessageOK)
					response.ReturnInfoMessage(&conn, proto.MessageEndSync)
					continue
				} else if message.Type == proto.MessageCloseConnection {

					ok := connection.Token == message.Token

					if !ok {
						s.evProc.Send(EventType("yellow"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v) wrong token", conn.RemoteAddr()))

						bytesSent, err := response.ReturnError(&conn, proto.ErrAccessDenied)
						if err != nil {
							s.evProc.Send(EventType("error"), events.SourceSyncServerListener.String(), fmt.Errorf("(%v) error making response: %w", conn.RemoteAddr(), err))
							if connection.ServerErrors >= s.config.MaxServerErrors && s.config.MaxServerErrors > 0 {
								s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
								s.CloseConnection(&connection, connStateChannel)
								return
							}
							continue
						}
						if s.config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.config.ServerVerboseLogging && !s.config.SilentMode {
							s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}
						continue
					}

					// Requesting to start syncing back
					response.ReturnInfoMessage(&conn, proto.MessageOK)
					s.CloseConnection(&connection, connStateChannel)
					return
				} else if message.Type == proto.MessageGetFile {
					// Create separate stream to send a file
					fmt.Println("GET FILE")
					getFile, err := message.ParseGetFile()
					if err != nil {
						fmt.Println(err)
					}

					go func() {
						_, err := message.SendFile(conn, s.fw.UnEscapeAddress(s.fw.InsertUser(filepath.Join(getFile.Path, getFile.Name), connection.Uid)), s.fw, connection.Uid)
						if err != nil {
							fmt.Println(err)
						}
						fmt.Println("SENT FILE")
					}()

					continue
				}

			}

			if s.config.CountStats {
				fmt.Println(recievedBytes)
			}

			s.CloseConnection(&connection, connStateChannel)

		}(conn)
	}
}

func (s *Server) CloseConnection(connection *ActiveConnection, connStateChannel chan ConnectionEvent) {
	s.connectionPoolMutex.Lock()
	s.activeConnectionsNumber--
	// Close sync in pool, so we can trash it
	s.activeConnections[connection.NumerInPool].Active = false
	s.activeConnections[connection.NumerInPool].SyncActive = false
	s.activeConnections[connection.NumerInPool].DisconnectedAt = time.Now()
	s.connectionPoolMutex.Unlock()

	connStateChannel <- ConnectionClose

	if s.config.ServerVerboseLogging && !s.config.SilentMode {
		s.evProc.Send(EventType("info"), events.SourceSyncServerListener.String(), fmt.Sprintf("(%v)[Connection closed] - recieved %d bytes, sent %d bytes. Errors: %d. Active connections: %d", connection.IP, s.activeConnections[connection.NumerInPool].BytesRecieved, s.activeConnections[connection.NumerInPool].BytesSent, s.activeConnections[connection.NumerInPool].ClientErrors+s.activeConnections[connection.NumerInPool].ServerErrors, s.activeConnectionsNumber))
	}
}
