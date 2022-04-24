package sync

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"

	"github.com/fsnotify/fsnotify"
)

func (s *Server) Listen() {
	cert, err := tls.LoadX509KeyPair(s.Config.CertPath, s.Config.KeyPath)
	if err != nil {
		s.Logger.Fatal("Error getting key pair: ", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	s.Logger.InfoCyan(fmt.Sprintf("Listening on %s:%s", s.Config.HostName, s.Config.Port))

	//TO DO: add timeout
	l, err := tls.Listen("tcp", ":"+s.Config.Port, tlsConfig)
	if err != nil {
		s.Logger.Fatal("Error listening: ", err)
	}
	defer func() {
		err := l.Close()
		if err != nil {
			s.Logger.Error("Error closing connection:", err)
		}
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			s.Logger.Error("Error accepting connection: ", err)
		}
		if s.Config.ServerVerboseLogging {
			s.Logger.Info(conn.RemoteAddr(), " - new connection. Active: ", len(s.ActiveConnections)+1)
		}

		go func(c net.Conn) {
			eventsChannel := make(chan fsnotify.Event)
			connClosedChannel := make(chan bool)
			connection := ActiveConnection{
				EventsChan: eventsChannel,
				IP:         c.RemoteAddr(),
				Closed:     connClosedChannel,
			}
			s.ChannelMutex.Lock()
			s.ActiveConnections = append(s.ActiveConnections, connection)
			s.ChannelMutex.Unlock()

			var message Message
			var response Message
			var recievedBytes int
			var bytesSent int

			recievedBytes, err := message.Read(c)
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

			if s.Config.CountStats {
				fmt.Println(recievedBytes)
			}

			// Process authorization
			if message.Type == MessageAuth {
				newToken, err := message.ProcessFullAuth(&conn, s.DB)
				if err != nil {
					s.Logger.Error(conn.RemoteAddr(), " - error parsing auth: ", err)

					bytesSent, err := response.ReturnError(&conn, ErrInternal)
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
					if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
						s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), recievedBytes, bytesSent))
					}
					return
				}

				if newToken == "" {
					s.Logger.Warn(conn.RemoteAddr(), " - wrong auth")

					bytesSent, err := response.ReturnError(&conn, ErrAccessDenied)
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
					if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
						s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), recievedBytes, bytesSent))
					}
					return
				}

				// Return token to client
				bytesSent, err := response.ReturnToken(&conn, newToken)
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
				if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
					s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), recievedBytes, bytesSent))
				}

				/*	for event := (<-s.ConnNotifier); event.Name != ""; {
					io.WriteString(c, fmt.Sprintf("%s %s", event.Name, event.Op))
				}*/
				for {
					select {
					case event, ok := <-eventsChannel:
						if !ok {
							io.WriteString(c, "Channel closed")
							return
						}
						fmt.Println("Recieved")
						io.WriteString(c, fmt.Sprintf("%s %s", event.Name, event.Op))
						//s.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))
						/*
							case err, ok := <-s.Watcher.Errors:
								if !ok {
									io.WriteString(c, "Channel closed")
									return
								}
								s.Logger.Error("FS watcher error: ", err)*/

					default:
						// Persist connection
						//time.Sleep(150 * time.Second)
					}
				}

				return
			} else if message.Type == MessageDirSyncReq { // Client requested to sync some directory

				// Вернуть список изменений с момента последнего подключения этого клиента
				// Если папка была создана после этого момента - вернуть всю папку

			}

			fmt.Println(message)

			io.WriteString(c, "string(response)")

			if s.Config.ServerVerboseLogging && !s.Config.SilentMode {
				s.Logger.Info(fmt.Sprintf("%v - recieved %d bytes, sent %d bytes", conn.RemoteAddr(), recievedBytes, bytesSent))
			}

			// Logging for debug and stats

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
