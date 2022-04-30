package sync

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"time"
)

func (s *Server) Listen() {
	cert, err := tls.LoadX509KeyPair(s.Config.CertPath, s.Config.KeyPath)
	if err != nil {
		s.Logger.Fatal("[Listen] error getting key pair -> ", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	s.Logger.InfoCyan(fmt.Sprintf("Listening on %s:%s", s.Config.HostName, s.Config.Port))

	//TO DO: add timeout
	l, err := tls.Listen("tcp", ":"+s.Config.Port, tlsConfig)
	if err != nil {
		s.Logger.Fatal("[Listen] error listening -> ", err)
	}
	defer func() {
		err := l.Close()
		if err != nil {
			s.Logger.Error("Error closing connection -> ", err)
		}
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			s.Logger.Error("[Listen] error accepting connection -> ", err)
		}
		if s.Config.ServerVerboseLogging {
			s.Logger.Info(conn.RemoteAddr(), " - new connection. Active: ", s.ActiveConnectionsNumber+1)
		}

		go func(c net.Conn) {
			eventsChannel := make(chan ConnNotifierEvent)
			connStateChannel := make(chan ConnectionEvent)
			connection := ActiveConnection{
				EventsChan: eventsChannel,
				IP:         c.RemoteAddr(),
				ConnectAt:  time.Now(),
				StateChan:  connStateChannel,
				Active:     true,
			}
			s.ChannelMutex.Lock()
			connection.NumerInPool = len(s.ActiveConnections)
			s.ActiveConnections = append(s.ActiveConnections, connection)
			s.ActiveConnectionsNumber++
			s.ChannelMutex.Unlock()

			var message Message
			var response Message
			var recievedBytes uint

			response.Token = s.Config.ServerToken
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
							s.ChannelMutex.Lock()
							s.ActiveConnections[connection.NumerInPool].SyncActive = false
							s.ChannelMutex.Unlock()
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
							s.ChannelMutex.Lock()
							s.ActiveConnections[connection.NumerInPool].SyncActive = true
							s.ChannelMutex.Unlock()
							s.Logger.Info(conn.RemoteAddr(), " - sync started")

						} else if state == ConnectionSyncStop {
							fmt.Println("Sync stopped")
							s.ChannelMutex.Lock()
							s.ActiveConnections[connection.NumerInPool].SyncActive = false
							s.ChannelMutex.Unlock()
							s.Logger.Info(conn.RemoteAddr(), " - sync stopped")
						}
					}
				}
			}()

			for {
				netData, err := bufio.NewReader(c).ReadBytes(MessageTerminator)
				if err != nil {

					// If connection closed - break the cycle
					if errors.As(err, &io.ErrClosedPipe) {
						s.Logger.Info(fmt.Sprintf("(%v) - conn closed by other party", conn.RemoteAddr()))
						s.CloseConnection(&connection, connStateChannel)
						break
					}
					s.Logger.Error(fmt.Sprintf("(%v)[ReadBytes] - error reading data: %v", conn.RemoteAddr(), err))
					continue
				}

				err = message.Parse(&netData)
				if err != nil {
					s.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn.RemoteAddr(), err))
					if connection.ClientErrors >= s.Config.MaxClientErrors {
						s.CloseConnection(&connection, connStateChannel)
					}
					continue
				}

				fmt.Println("client read:", string(netData))
				fmt.Println("client read:", message)

				if message.Type == MessageAuth {
					newToken, err := message.ProcessFullAuth(&conn, s.DB, s.Config.TokenValidDays)
					if err != nil {
						s.Logger.Error(fmt.Sprintf("(%v)[message.ProcessFullAuth] - error parsing auth: %v", conn.RemoteAddr(), err))

						bytesSent, err := response.ReturnError(&conn, ErrInternal)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
							s.CloseConnection(&connection, connStateChannel)
							continue
						}
						if s.Config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
							s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					if newToken == "" {
						s.Logger.InfoYellow(conn.RemoteAddr(), " - wrong auth")

						bytesSent, err := response.ReturnError(&conn, ErrAccessDenied)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
							s.CloseConnection(&connection, connStateChannel)
							continue
						}
						if s.Config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
							s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					// Return token to client
					bytesSent, err := response.ReturnToken(&conn, newToken)
					if err != nil {
						s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
						s.CloseConnection(&connection, connStateChannel)
						continue
					}
					if s.Config.CountStats {
						fmt.Println(bytesSent)
					}
					if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
						s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
					}

					// Token persists in server memory only
					connection.Token = newToken
					continue
				} else if message.Type == MessageOK {
					ok, err := message.ValidateOK()
					if err != nil {
						s.Logger.Error(conn.RemoteAddr(), " - error parsing OK: ", err)

						_, err := response.ReturnError(&conn, ErrBrokenMessage)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
						}
						if connection.ClientErrors >= s.Config.MaxClientErrors {
							s.CloseConnection(&connection, connStateChannel)
						}
						continue

					}
					if !ok.OK {
						s.Logger.Warn(conn.RemoteAddr(), " - client sent error: ", ok.HumanReadable)
						continue
					}
					continue
				} else if message.Type == MessageStartSync {

					ok := connection.Token == message.Token

					if !ok {
						s.Logger.InfoYellow(conn.RemoteAddr(), " - wrong token")

						bytesSent, err := response.ReturnError(&conn, ErrAccessDenied)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
							if connection.ServerErrors >= s.Config.MaxServerErrors && s.Config.MaxServerErrors > 0 {
								s.Logger.Warn(fmt.Sprintf("(%v)[Config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
								s.CloseConnection(&connection, connStateChannel)
								return
							}
							continue
						}
						if s.Config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
							s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					// Starting sync channel with the other party
					connStateChannel <- ConnectionSyncStart
					// Requesting to start syncing back
					response.ReturnInfoMessage(&conn, MessageOK)
					response.ReturnInfoMessage(&conn, MessageStartSync)
					continue
				} else if message.Type == MessageEndSync {

					ok := connection.Token == message.Token

					if !ok {
						s.Logger.InfoYellow(conn.RemoteAddr(), " - wrong token")

						bytesSent, err := response.ReturnError(&conn, ErrAccessDenied)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
							if connection.ServerErrors >= s.Config.MaxServerErrors && s.Config.MaxServerErrors > 0 {
								s.Logger.Warn(fmt.Sprintf("(%v)[Config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
								s.CloseConnection(&connection, connStateChannel)
								return
							}
							continue
						}
						if s.Config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
							s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					// Starting sync channel with the other party
					connStateChannel <- ConnectionSyncStop
					// Requesting to start syncing back
					response.ReturnInfoMessage(&conn, MessageOK)
					response.ReturnInfoMessage(&conn, MessageEndSync)
					continue
				} else if message.Type == MessageCloseConnection {

					ok := connection.Token == message.Token

					if !ok {
						s.Logger.InfoYellow(conn.RemoteAddr(), " - wrong token")

						bytesSent, err := response.ReturnError(&conn, ErrAccessDenied)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
							if connection.ServerErrors >= s.Config.MaxServerErrors && s.Config.MaxServerErrors > 0 {
								s.Logger.Warn(fmt.Sprintf("(%v)[Config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
								s.CloseConnection(&connection, connStateChannel)
								return
							}
							continue
						}
						if s.Config.CountStats {
							fmt.Println(bytesSent)
						}
						if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
							s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), len(netData), bytesSent))
						}

						continue
					}

					// Requesting to start syncing back
					response.ReturnInfoMessage(&conn, MessageOK)
					s.CloseConnection(&connection, connStateChannel)
					return
				} else if message.Type == MessageGetFile {
					// Create separate stream to send a file
					fmt.Println("GET FILE")
					getFile, err := message.ParseGetFile()
					if err != nil {
						fmt.Println(err)
					}
					go func() {
						_, err := message.SendFile(conn, s.FW.UnEscapeAddress(filepath.Join(getFile.Path, getFile.Name)), s.FW)
						if err != nil {
							fmt.Println(err)
						}
						fmt.Println("SENT FILE")
					}()

					continue
				}

			}

			if s.Config.CountStats {
				fmt.Println(recievedBytes)
			}

			s.CloseConnection(&connection, connStateChannel)

		}(conn)
	}
}

func (s *Server) CloseConnection(connection *ActiveConnection, connStateChannel chan ConnectionEvent) {
	s.ChannelMutex.Lock()
	s.ActiveConnectionsNumber--
	// Close sync in pool, so we can trash it
	s.ActiveConnections[connection.NumerInPool].Active = false
	s.ActiveConnections[connection.NumerInPool].SyncActive = false
	s.ActiveConnections[connection.NumerInPool].DisconnectedAt = time.Now()
	s.ChannelMutex.Unlock()

	connStateChannel <- ConnectionClose

	if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
		s.Logger.Info(fmt.Sprintf("(%v)[Connection closed] - recieved %d bytes, sent %d bytes. Errors: %d. Active connections: %d", connection.IP, s.ActiveConnections[connection.NumerInPool].BytesRecieved, s.ActiveConnections[connection.NumerInPool].BytesSent, s.ActiveConnections[connection.NumerInPool].ClientErrors+s.ActiveConnections[connection.NumerInPool].ServerErrors, s.ActiveConnectionsNumber))
	}
}
