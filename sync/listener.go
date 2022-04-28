package sync

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/pcloud-sync-server/users"
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
			eventsChannel := make(chan fsnotify.Event)
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
			//var bytesSent int

			// Routine to maintain current connection
			go func() {
				for {
					select {
					case event, ok := <-eventsChannel:
						if !ok {
							io.WriteString(c, "Channel closed")
							// Close sync in pool
							s.ChannelMutex.Lock()
							s.ActiveConnections[connection.NumerInPool].SyncActive = false
							s.ChannelMutex.Unlock()
							return
						}
						fmt.Println("Sending to client")
						io.WriteString(c, fmt.Sprintf("%s %s", s.EscapeAddress(event.Name), event.Op))

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
					/*if neterr, ok := err.(net.Error); ok {
						fmt.Println("SSSasdasdasdS", neterr)
						fmt.Printf("%T %+v", err, err)
						break // connection will be closed
					}*/

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

				} else if message.Type == MessageStartSync {
					fmt.Println(message.Token)
					ok, err := users.ValidateToken(message.Token, s.DB)
					if err != nil {
						s.Logger.Error(fmt.Sprintf("(%v)[MessageStartSync] - error parsing auth: %v", conn.RemoteAddr(), err))

						_, err := response.ReturnError(&conn, ErrBrokenMessage)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
						}
						if connection.ServerErrors >= s.Config.MaxServerErrors && s.Config.MaxServerErrors > 0 {
							s.Logger.Error(conn.RemoteAddr(), " - server error per connection limit, conn will be closed: ")
							s.CloseConnection(&connection, connStateChannel)
							return
						}
						continue
					}

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
					response.ReturnInfoMessage(&conn, s.Config.ServerToken, MessageOK)
					response.ReturnInfoMessage(&conn, s.Config.ServerToken, MessageStartSync)
				} else if message.Type == MessageEndSync {
					ok, err := users.ValidateToken(message.Token, s.DB)
					if err != nil {
						s.Logger.Error(fmt.Sprintf("(%v)[MessageEndSync] - error parsing auth: %v", conn.RemoteAddr(), err))

						_, err := response.ReturnError(&conn, ErrBrokenMessage)
						if err != nil {
							s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
						}
						if connection.ServerErrors >= s.Config.MaxServerErrors && s.Config.MaxServerErrors > 0 {
							s.Logger.Warn(fmt.Sprintf("(%v)[Config.MaxServerErrors] - 'client error per connection' limit reached, conn will be closed", conn.RemoteAddr()))
							s.CloseConnection(&connection, connStateChannel)
							return
						}
						continue
					}

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
					response.ReturnInfoMessage(&conn, s.Config.ServerToken, MessageOK)
					response.ReturnInfoMessage(&conn, s.Config.ServerToken, MessageEndSync)
				}

			}

			//time.Sleep(1 * time.Second)
			// Closing the sync routine

			if s.Config.CountStats {
				fmt.Println(recievedBytes)
			}

			/*s.ChannelMutex.Lock()
			s.ActiveConnectionsNumber--
			s.Logger.Info(fmt.Sprintf("(%v)[Connection closed] - recieved %d bytes, sent %d bytes. Active connections: %d", conn.RemoteAddr(), 0, 0, s.ActiveConnectionsNumber))
			s.ChannelMutex.Unlock()*/

			s.CloseConnection(&connection, connStateChannel)

			/*temp := strings.TrimSpace(string(netData))
			if temp == ConnectionCloser {
				return
			}*/

			/*recievedBytes, err := message.Read(c)
			if err != nil {
				s.Logger.Error(conn.RemoteAddr(), " - error reading data: ", err)

				bytesSent, err := response.ReturnError(&conn, ErrBrokenMessage)
				if err != nil {
					s.Logger.Error(conn.RemoteAddr(), " - error making response: ", err)
					if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
						s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), recievedBytes, bytesSent))
					}
					return
				}

				if s.Config.CountStats {
					fmt.Println(bytesSent)
				}
			}

			fmt.Println(message)
			io.WriteString(c, "string(some response)")

			if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
				s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), recievedBytes, bytesSent))
			}*/

			// Process authorization
			/*if message.Type == MessageAuth {
			} else if message.Type == MessageDirSyncReq { // Client requested to sync some directory

				// Вернуть список изменений с момента последнего подключения этого клиента
				// Если папка была создана после этого момента - вернуть всю папку

			}*/

			/*
				//the only message type allowed without token is "auth"
				if messageRecieved.Type != "auth" {
					if messageRecieved.Token == "" {
						logger.Warn("No token: ", conn.RemoteAddr())

						if retErr := returnError(3, config.Current.ServerToken, &c); retErr != nil {
							logger.Error("Error making response: ", err)
						}
						return
					}

					ok, err := users.ValidateToken(messageRecieved.Token, db)
					if err != nil {
						logger.Warn("Error validating token: ", err, conn.RemoteAddr())

						if retErr := returnError(3, config.Current.ServerToken, &c); retErr != nil {
							logger.Error("Error making response: ", err)
						}
						return
					}

					if !ok {
						if config.Current.ServerVerboseLogging {
							logger.InfoYellow(conn.RemoteAddr(), " - incorrect token")
						}

						if retErr := returnError(3, config.Current.ServerToken, &c); retErr != nil {
							logger.Error("Error making response: ", err)
						}
						return
					}

					//scan all files in root dir
					/*err = filesystem.ScanDirToDB(db, appConfig.FileSystemRootPath, watcher)
					if err != nil {
						logger.Zap.Error("Error scanning dir: ", err)
					}
					//compare files from client with files on server
					//return correct list

					io.WriteString(c, "string(response)")

				}*/
			/*
				//now implement actions needed
				if messageRecieved.Type == "sync" { //if client wants to sync

				} else if messageRecieved.Type == "auth" { //client sends creds to auth
					//empty creds not allowed
					if messageRecieved.Auth.Login == "" || messageRecieved.Auth.Password == "" {
						logger.Error("No credentials provided: ", conn.RemoteAddr())

						if retErr := returnError(3, config.Current.ServerToken, &c); retErr != nil {
							logger.Error("Error making response: ", err)
						}
						return
					}

					ok, userId, err := users.ValidateUserCreds(messageRecieved.Auth.Login, messageRecieved.Auth.Password, db)
					if err != nil {
						logger.Error("Error validating credentials: ", err, conn.RemoteAddr())

						if retErr := returnError(3, config.Current.ServerToken, &c); retErr != nil {
							logger.Error("Error making response: ", err)
						}
						return
					}

					if !ok {
						logger.Info("Wrong login or password: ", conn.RemoteAddr())
						if retErr := returnError(3, config.Current.ServerToken, &c); retErr != nil {
							logger.Error("Error making response: ", err.Error())
						}
						return
					}

					//if everything fine - generate token
					token, err := users.GenerateToken()
					if err != nil {
						logger.Error(fmt.Sprintf("Error generating token: %v", err))

						if retErr := returnError(4, config.Current.ServerToken, &c); retErr != nil {
							logger.Error(fmt.Sprintf("Error making response: %v", err))
						}
						return
					}

					err = users.RegisterToken(userId, token, db)
					if err != nil {
						logger.Error(fmt.Sprintf("Error registering token: %v", err))

						if retErr := returnError(4, config.Current.ServerToken, &c); retErr != nil {
							logger.Error(fmt.Sprintf("Error making response: %v", err))
						}
						return
					}

					//return new token to client
					respStruct := Message{
						Type:      "new_token",
						Token:     config.Current.ServerToken,
						Auth:      Auth{NewToken: token},
						TimeStamp: time.Now(),
					}
					response, err := json.Marshal(respStruct)
					if err != nil {
						logger.Error(fmt.Sprintf("Error marshaling message: %v", err))

						if retErr := returnError(4, config.Current.ServerToken, &c); retErr != nil {
							logger.Error(fmt.Sprintf("Error making response: %v", retErr))
						}
						return
					}
					io.WriteString(c, string(response))

					return

				} else if messageRecieved.Type == "new_file" {

				} else if messageRecieved.Type == "get_file" {

				} else {
					logger.Info("Broken message: ", err, conn.RemoteAddr())

					if retErr := returnError(2, config.Current.ServerToken, &c); retErr != nil {
						logger.Error("Error making response: ", err.Error())
					}
					return
				}*/

			//check if this client has right token

			//if not - demand credentials

			//compare existing files on the server with files on the client

			//send actual file list (if no new files on server)

			//demand some files from client if it has newer versions or brand new files
			//respond to client
			//io.WriteString(c, "Got your message!\n")

		}(conn)
	}
}

func (s *Server) CloseConnection(connection *ActiveConnection, connStateChannel chan ConnectionEvent) {
	s.ChannelMutex.Lock()
	s.ActiveConnectionsNumber--
	// Close sync in pool, so we can trash it
	s.ActiveConnections[connection.NumerInPool].Active = false
	s.ActiveConnections[connection.NumerInPool].SyncActive = false
	s.ChannelMutex.Unlock()

	connStateChannel <- ConnectionClose

	if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
		s.Logger.Info(fmt.Sprintf("(%v)[Connection closed] - recieved %d bytes, sent %d bytes. Active connections: %d", connection.IP, 0, 0, s.ActiveConnectionsNumber))
	}
}
