package client

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofrs/uuid"
	"github.com/lazybark/go-helpers/hasher"
	"github.com/lazybark/go-pretty-code/logs"
	"github.com/lazybark/pcloud-sync-server/cloud"
	"github.com/lazybark/pcloud-sync-server/cloud/proto"
	"github.com/lazybark/pcloud-sync-server/fsworker"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	Version = "0.0.0-alpha 2022.04.29"
)

// NewSyncServer creates a sync server instance
func NewSyncClient() *Client {
	return new(Client)
}

func (c *Client) Start() {
	c.TimeStart = time.Now()
	c.AppVersion = Version
	c.ActionsBuffer = make(map[string][]BufferedAction)

	err := c.LoadConfig(".")
	if err != nil {
		log.Fatal("Error getting config: ", err)
	}

	// Connect Logger
	logfileName := fmt.Sprintf("pcloud-sync-server_%v-%v-%v_%v-%v-%v.log", c.TimeStart.Year(), c.TimeStart.Month(), c.TimeStart.Day(), c.TimeStart.Hour(), c.TimeStart.Minute(), c.TimeStart.Second())
	logger, err := logs.Double(filepath.Join(c.Config.LogDirMain, logfileName), false, zap.InfoLevel)
	if err != nil {
		log.Fatal("Error getting logger: ", err)
	}
	c.Logger = logger

	c.Logger.InfoCyan("App Version: ", c.AppVersion)

	// Connect DB
	c.DB, err = cloud.OpenSQLite(c.Config.SQLiteDBName)
	if err != nil {
		c.Logger.Fatal("DB add failed:", err)
	}

	// Connect FS watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		c.Logger.Fatal("New Watcher failed:", err)
	}
	c.Watcher = watch

	// Force rescan filesystem and flush old DB-records
	c.InitDB()
	if err != nil {
		c.Logger.Fatal("Error flushing DB: ", err)
	}

	// New filesystem worker
	c.FW = fsworker.NewWorker(c.Config.FileSystemRootPath, c.DB, watch)

	// Watch root dir
	c.Logger.InfoGreen("Starting watcher")
	go c.FilesystemWatcherRoutine()

	// Process and watch all subdirs
	_, _, err = c.FW.ProcessDirectory(c.Config.FileSystemRootPath)
	if err != nil {
		c.Logger.Fatal("Error processing FS: ", err)
	}
	c.Logger.InfoGreen("Root directory processed. Total time: ", time.Since(c.TimeStart))

	c.Logger.InfoGreen("Starting client")

	c.Sync()
}

func (c *Client) InitTLSConnection() (conn *tls.Conn, err error) {
	cert, err := os.ReadFile(c.Config.ServerCert)
	if err != nil {
		return conn, fmt.Errorf("[os.ReadFile] unable to read file -> %w", err)
	}
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(cert); !ok {
		return conn, fmt.Errorf("[x509.NewCertPool] unable to parse cert from %s -> %w", c.Config.ServerCert, err)
	}
	config := &tls.Config{RootCAs: certPool}
	// Step 1: init connect with specified server
	conn, err = tls.Dial("tcp", fmt.Sprintf("%s:%d", c.Config.ServerAddress, c.Config.ServerPort), config)
	if err != nil {
		return conn, fmt.Errorf("[tls.Dial] unable to dial to %s:%d -> %w", c.Config.ServerAddress, c.Config.ServerPort, err)
	}

	return
}

func (c *Client) Sync() {
	conn, err := c.InitTLSConnection()
	if err != nil {
		c.Logger.FatalBackRed("[Sync] can not init connection -> %w", err)
	}
	// Step 2: send auth message as hello and wait for token
	var message proto.Message
	//message.AppVersion = c.AppVersion
	c.Logger.Info(fmt.Sprintf("Sending hello to %v", conn.RemoteAddr()))
	bytesSent, err := message.SendHello(conn, proto.SyncIntensionClient, "test_client_1", c.AppVersion, "lazybark.dev@gmail.com", 0, 15, 2048)
	if err != nil {
		c.Logger.Error(" - error making response: ", err)
	}

	for {
		netData, err := bufio.NewReader(conn).ReadBytes(proto.MessageTerminator)
		if err != nil {

			// If connection closed - break the cycle
			if errors.As(err, &io.ErrClosedPipe) {
				c.Logger.Info(fmt.Sprintf("(%v) - conn closed by other party", conn.RemoteAddr()))
				break
			}
			c.Logger.Error(fmt.Sprintf("(%v)[ReadBytes] - error reading data: %v", conn.RemoteAddr(), err))
			continue
		}

		err = message.Parse(&netData)
		if err != nil {
			c.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn.RemoteAddr(), err))
			continue
		}

		if message.Type == proto.MessageHandshake {
			handshake, err := message.ParseHandshake()
			if err != nil {
				fmt.Println(err)
				continue
			}
			c.Logger.InfoGreen(fmt.Sprintf("Got handshake from %v. Server name %s (v %s). Max file size %d, max connections per user %d", conn.RemoteAddr(), handshake.PartyName, handshake.AppVersion, handshake.MaxFileSize, handshake.MaxConnectionsPerUser))
			c.Logger.InfoGreen(fmt.Sprintf("Server owner info: %s", handshake.OwnerContacts))

			break
		}

	}

	bytesSent, err = message.SendAuthMessage(conn, c.Config.Login, c.Config.Password, "", "", false)
	if err != nil {
		c.Logger.Error(" - error making response: ", err)
	}

	for {
		netData, err := bufio.NewReader(conn).ReadBytes(proto.MessageTerminator)
		if err != nil {

			// If connection closed - break the cycle
			if errors.As(err, &io.ErrClosedPipe) {
				c.Logger.Info(fmt.Sprintf("(%v) - conn closed by other party", conn.RemoteAddr()))
				break
			}
			c.Logger.Error(fmt.Sprintf("(%v)[ReadBytes] - error reading data: %v", conn.RemoteAddr(), err))
			continue
		}

		err = message.Parse(&netData)
		if err != nil {
			c.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn.RemoteAddr(), err))
			continue
		}

		if message.Type == proto.MessageToken {
			newToken, err := message.ParseNewToken()
			if err != nil {
				c.Logger.Error(fmt.Sprintf("[MessageToken] - broken message -> %v", err))
				continue
			}
			c.CurrentToken = newToken.Token

			if !c.SyncActive {
				c.SyncActive = true
				c.FileGetter = make(chan proto.GetFile)
			}
			go func() {
				fmt.Println("CONNECTED")
				for {
					select {
					case fileToGet, ok := <-c.FileGetter:
						if !ok {
							return
						}
						fmt.Println("Getting", fileToGet.Name)

						c.FilesInRow = append(c.FilesInRow, fileToGet)
						go c.GetFile(&fileToGet)
					}
				}
			}()

			break
		} else if message.Type == proto.MessageError {
			errServer, err := message.ParseError()
			if err != nil {
				c.Logger.Error(fmt.Sprintf("[MessageError] - broken message -> %v", err))
				continue
			}
			if errServer.Type == proto.ErrInternal {
				c.Logger.Error("[SYNC] - internal server error")
				return
			} else if errServer.Type == proto.ErrAccessDenied {
				c.Logger.Error("[SYNC] - access denied. Check server address, port and user credentials")
				return
			} else if errServer.Type == proto.ErrTooMuchClientErrors {
				c.Logger.Error(fmt.Sprintf("[SYNC] - %s", errServer.Type.String()))
				return
			} else if errServer.Type == proto.ErrTooMuchServerErrors {
				c.Logger.Error(fmt.Sprintf("[SYNC] - %s", errServer.Type.String()))
				return
			}
		} else {
			// TO DO: send not ok
			continue
		}
	}

	// Step 3: ask for sync
	message.Token = c.CurrentToken

	bytesSent, err = message.ReturnInfoMessage(conn, proto.MessageStartSync)
	if err != nil {
		c.Logger.Error(" - error making response: ", err)
	}
	fmt.Println("sent sync start", bytesSent)

	for {
		netData, err := bufio.NewReader(conn).ReadBytes(proto.MessageTerminator)
		if err != nil {
			fmt.Println("client read err:", string(netData))

			// If connection closed - break the cycle
			if errors.As(err, &io.ErrClosedPipe) {
				c.Logger.Info(fmt.Sprintf("(%v) - conn closed by other party", conn.RemoteAddr()))
				break
			}
			c.Logger.Error(fmt.Sprintf("(%v)[ReadBytes] - error reading data: %v", conn.RemoteAddr(), err))
			continue
		}

		err = message.Parse(&netData)
		if err != nil {
			c.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn.RemoteAddr(), err))
			continue
		}

		if message.Type == proto.MessageOK {
			fmt.Println("SERVER RESPONSE: ", message.Type)
		} else if message.Type == proto.MessageSyncEvent {
			fmt.Println("SYNC EVENT")
			event, err := message.ParseSyncEvent()
			if err != nil {
				c.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn.RemoteAddr(), err))
				continue
			}

			// Process removing
			if event.Type == proto.ObjectRemoved {
				c.ActionsBufferMutex.Lock()
				c.ActionsBuffer[c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name))] = append(c.ActionsBuffer[c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name))],
					BufferedAction{
						Action:    fsnotify.Remove,
						Timestamp: time.Now(),
					})
				c.ActionsBufferMutex.Unlock()
				dat, err := os.Stat(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)))
				if err != nil {
					c.Logger.Error("Object reading failed: ", c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)), err)
					continue
				}

				if dat.ModTime().After(event.NewUpdatedAt) {
					c.Logger.Warn("We have newer version of ", filepath.Join(event.Path, event.Name))
					continue
				}

				var file fsworker.File
				var folder fsworker.Folder

				if event.ObjectType == proto.ObjectDir {

					err = c.DB.Where("name = ? and path = ?", event.Name, event.Path).First(&folder).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						c.Logger.Error(err)
					}
					err = c.DB.Delete(&folder).Error
					if err != nil {
						c.Logger.Error(err)
					}
					// Manually delete all files connected to this dir
					err = c.DB.Where("path = ?", c.FW.EscapeAddress(event.Name)).Delete(&file).Error
					if err != nil {
						c.Logger.Error(err)
					}

					err = os.RemoveAll(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)))
					if err != nil {
						c.Logger.Error(err)
						continue
					}

				} else if event.ObjectType == proto.ObjectFile {

					err := c.DB.Where("name = ? and path = ?", event.Name, event.Path).First(&file).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						c.Logger.Error(err)
					}
					err = c.DB.Delete(&file).Error
					if err != nil {
						c.Logger.Error(err)
					}

					err = os.RemoveAll(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)))
					if err != nil {
						c.Logger.Error(err)
						continue
					}
				}
				continue
			} else if event.Type == proto.ObjectCreated {
				fmt.Println("CREATED")
				if event.ObjectType == proto.ObjectDir {
					address := c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name))
					fmt.Println("DIR", address)
					if err := os.MkdirAll(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)), os.ModePerm); err != nil {
						c.Logger.Error(err)
						continue
					}
					err = os.Chtimes(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)), event.NewUpdatedAt, event.NewUpdatedAt)
					if err != nil {
						fmt.Println(err)
					}

					// Scan dir
					_, _, err = c.FW.ProcessDirectory(address)
					if err != nil {
						c.Logger.Error(fmt.Sprintf("Error processing %s: ", event.Name), err)
					}

					dat, err := os.Stat(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)))
					if err != nil {
						c.Logger.Error("Object reading failed: ", c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)), err)
						continue
					}

					// Add object to DB
					err = c.FW.MakeDBRecord(dat, address)
					if err != nil && err.Error() != "UNIQUE constraint failed: name, path" {
						c.Logger.Error(fmt.Sprintf("Error making record for %s: ", event.Name), err)
					}
					/*****************************************************/
					/*****************************************************/
					/*****************************************************/
					/*****************************************************/
					/* And here we will ask server to send full list of directory files in case in was crated not empty*/
					/*****************************************************/
					/*****************************************************/
					/*****************************************************/

				} else if event.ObjectType == proto.ObjectFile {
					fmt.Println("FILE", c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)))
					fileToGet := proto.GetFile{
						Name:      event.Name,
						Path:      event.Path,
						Hash:      event.Hash,
						UpdatedAt: event.NewUpdatedAt,
					}
					c.FileGetter <- fileToGet
					/*
						if err := os.MkdirAll(c.FW.UnEscapeAddress(filepath.Join(event.Path)), os.ModePerm); err != nil {
							c.Logger.Error(err)
							continue
						}
						myfile, e := os.Create(c.FW.UnEscapeAddress(filepath.Join(event.Path, event.Name)))
						if e != nil {
							log.Fatal(e)
						}
						myfile.Close()*/
				}
			} else if event.Type == proto.ObjectUpdated {
				fmt.Println("ObjectUpdated")

			}
		} else if message.Type == proto.MessageSendFile {
			fmt.Println("SERVER WANTS TO SEND A FILE")
			file, err := message.ParseFileData()
			if err != nil {
				fmt.Println(err)
			}

			c.ActionsBufferMutex.Lock()
			c.ActionsBuffer[c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name))] = append(c.ActionsBuffer[c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name))],
				BufferedAction{
					Action:    fsnotify.Create,
					Timestamp: time.Now(),
				})
			c.ActionsBufferMutex.Unlock()

			fmt.Println(c.ActionsBuffer[c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name))])

			if err := os.MkdirAll(c.FW.UnEscapeAddress(filepath.Join(file.Path)), os.ModePerm); err != nil {
				c.Logger.Error(err)
				continue
			}
			/*myfile, e := os.Create(c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name)))
			if e != nil {
				log.Fatal(e)
			}*/

			err = ioutil.WriteFile(c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name)), file.Data, 0644)
			if err != nil {
				return
			}

			err = os.Chtimes(c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name)), file.FSUpdatedAt, file.FSUpdatedAt)
			if err != nil {
				fmt.Println(err)
			}

			dat, err := os.Stat(c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name)))
			if err != nil {
				c.Logger.Error("Object reading failed: ", c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name)), err)
				continue
			}

			// Add object to DB
			err = c.FW.MakeDBRecord(dat, c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name)))
			if err != nil && err.Error() != "UNIQUE constraint failed: files.name, files.path" {
				c.Logger.Error(fmt.Sprintf("Error making record for %s: ", file.Name), err)
			}

			/*r := io.NewReader(file.Data)

			if _, err := io.Copy(myfile, file.Data); err != nil {
				return
			}*/

			/*_, err = myfile.Write(file.Data)
			if err != nil {
				fmt.Println(err)
			}*/

			//myfile.Close()
		} else {

			fmt.Println("SOME SHIT")
			fmt.Println(message)
		}

	}

}

func (c *Client) LoadConfig(path string) (err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("json")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&c.Config)
	if err != nil {
		return
	}

	return
}

// FilesystemWatcherRoutine tracks changes in every folder in root dir
func (c *Client) FilesystemWatcherRoutine() {
	done := make(chan bool)
	go func() {
		defer close(done)

		for {
			select {
			case event, ok := <-c.Watcher.Events:
				if !ok {
					return
				}

				if len(c.ActionsBuffer[event.Name]) > 0 {
					var skip bool
					for n, a := range c.ActionsBuffer[event.Name] {
						if a.Action == event.Op && !a.Skipped {
							skip = true
							fmt.Println("ACTION SKIPPED FOR ", event.Name)
							c.ActionsBufferMutex.Lock()
							c.ActionsBuffer[event.Name][n].Skipped = true
							c.ActionsBufferMutex.Unlock()
						}
					}
					if skip {

						continue
					}

				}

				select {
				case c.fsEventsChan <- event:

					c.Logger.Info(fmt.Sprintf("%s %s", event.Name, event.Op))

				default:

				}

				// Pause before processing actions to make sure that target isn't locked
				// If file hashing still produces errors (target busy) - increase pause time
				time.Sleep(100 * time.Millisecond)

				if event.Op.String() == "CREATE" {
					dat, err := os.Stat(event.Name)
					if err != nil {
						c.Logger.Error("Object reading failed: ", err)
						break
					}
					if dat.IsDir() {
						// Watch new dir
						err := c.Watcher.Add(event.Name)
						if err != nil {
							c.Logger.Error("FS watcher add failed:", err)
						}
						c.Logger.Info(fmt.Sprintf("%s added to watcher", event.Name))

						// Scan dir
						_, _, err = c.FW.ProcessDirectory(event.Name)
						if err != nil {
							c.Logger.Error(fmt.Sprintf("Error processing %s: ", event.Name), err)
						}
					}
					// Add object to DB
					err = c.FW.MakeDBRecord(dat, event.Name)
					if err != nil && err.Error() != "UNIQUE constraint failed: sync_files.name, sync_files.path" {
						c.Logger.Error(fmt.Sprintf("Error making record for %s: ", event.Name), err)
					}
				} else if event.Op.String() == "WRITE" {
					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))
					dat, err := os.Stat(event.Name)
					if err != nil {
						c.Logger.Error("Object reading failed: ", event.Op.String(), err)
						break
					}
					if dat.IsDir() {
						var folder fsworker.Folder

						if err := c.DB.Where("name = ? and path = ?", child, c.FW.EscapeAddress(dir)).First(&folder).Error; err != nil {
							c.Logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							folder.FSUpdatedAt = dat.ModTime()
							if err := c.DB.Table("folders").Save(&folder).Error; err != nil && err != gorm.ErrEmptySlice {
								c.Logger.Error("Dir saving failed: ", err)
							}
						}
					} else {
						var file fsworker.File
						if err := c.DB.Where("name = ? and path = ?", child, c.FW.EscapeAddress(dir)).First(&file).Error; err != nil {
							c.Logger.Error("File reading failed: ", err)
						} else {
							// Update data in DB
							hash := ""
							hash, err := hasher.HashFilePath(event.Name, hasher.SHA256, 8192)
							if err != nil {
								c.Logger.Error("3", err)
							}
							file.FSUpdatedAt = dat.ModTime()
							file.Size = dat.Size()
							file.Hash = hash
							if err := c.DB.Table("files").Save(&file).Error; err != nil && err != gorm.ErrEmptySlice {
								c.Logger.Error("Dir saving failed: ", err)
							}
						}
					}
				} else if event.Op.String() == "REMOVE" || event.Op.String() == "RENAME" { //no difference for DB between deletion and renaming
					var file fsworker.File
					var folder fsworker.Folder

					dir, child := filepath.Split(event.Name)
					dir = strings.TrimSuffix(dir, string(filepath.Separator))

					err := c.DB.Where("name = ? and path = ?", child, c.FW.EscapeAddress(dir)).First(&file).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						c.Logger.Error(err)
					}
					// ID becomes real if object found in DB
					if file.ID > 0 {
						err = c.DB.Delete(&file).Error
						if err != nil {
							c.Logger.Error(err)
						}
						break
					}

					err = c.DB.Where("name = ? and path = ?", child, c.FW.EscapeAddress(dir)).First(&folder).Error
					if err != nil && err != gorm.ErrRecordNotFound {
						c.Logger.Error(err)
					}
					// ID becomes real if object found in DB
					if folder.ID > 0 {
						err = c.DB.Delete(&folder).Error
						if err != nil {
							c.Logger.Error(err)
						}
						// Manually delete all files connected to this dir
						err = c.DB.Where("path = ?", c.FW.EscapeAddress(event.Name)).Delete(&file).Error
						if err != nil {
							c.Logger.Error(err)
						}

						break
					}
				}

			case err, ok := <-c.Watcher.Errors:
				if !ok {
					return
				}
				c.Logger.Error("FS watcher error: ", err)
			}
		}
	}()

	err := c.Watcher.Add(c.Config.FileSystemRootPath)
	if err != nil {
		c.Logger.Fatal("FS watcher add failed: ", err)
	}
	c.Logger.Info(fmt.Sprintf("%s added to watcher", c.Config.FileSystemRootPath))
	<-done
}

func (c *Client) GetFile(fileToGet *proto.GetFile) {
	conn2, err := c.InitTLSConnection()
	if err != nil {
		c.Logger.FatalBackRed("[Sync] can not init connection -> %w", err)
	}
	defer conn2.Close()
	// Step 2: send auth message as hello and wait for token
	var message2 proto.Message

	_, err = message2.SendAuthMessage(conn2, c.Config.Login, c.Config.Password, "", "", false)
	if err != nil {
		c.Logger.Error(" - error making response: ", err)
	}

	_, err = message2.SendGetFile(conn2, *fileToGet)
	if err != nil {
		c.Logger.Error(" - error making response: ", err)
	}

	// Await answer with file
	fmt.Println("waiting for message")
	for {
		netData2, err := bufio.NewReader(conn2).ReadBytes(proto.MessageTerminator)
		if err != nil {

			// If connection closed - break the cycle
			if errors.As(err, &io.ErrClosedPipe) {
				c.Logger.Info(fmt.Sprintf("(%v) - conn closed by other party", conn2.RemoteAddr()))
				return
			}
			c.Logger.Error(fmt.Sprintf("(%v)[ReadBytes] - error reading data: %v", conn2.RemoteAddr(), err))
			continue
		}

		err = message2.Parse(&netData2)
		if err != nil {
			c.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn2.RemoteAddr(), err))
			continue
		}

		fullPath := new(string)
		var updatedAt *time.Time

		if message2.Type == proto.MessageSendFile {
			fmt.Println("SERVER WANTS TO SEND A FILE 2")
			file, err := message2.ParseFileData()
			if err != nil {
				fmt.Println(err)
			}

			*fullPath = c.FW.UnEscapeAddress(filepath.Join(file.Path, file.Name))
			updatedAt = &file.FSUpdatedAt
			u2, _ := uuid.NewV4()
			tempPath := c.Config.CacheDir + string(filepath.Separator) + "cache_getfile" + u2.String()

			fmt.Println(*fullPath)

			c.ActionsBufferMutex.Lock()
			c.ActionsBuffer[*fullPath] = append(c.ActionsBuffer[*fullPath],
				BufferedAction{
					Action:    fsnotify.Create,
					Timestamp: time.Now(),
				})
			c.ActionsBufferMutex.Unlock()

			fmt.Println(c.ActionsBuffer[*fullPath])

			if err := os.MkdirAll(c.Config.CacheDir, os.ModePerm); err != nil {
				c.Logger.Error(err)
				continue
			}
			theFile, e := os.Create(tempPath)
			if e != nil {
				log.Fatal(e)
			}
			defer theFile.Close()

			for {
				netData2, err := bufio.NewReader(conn2).ReadBytes(proto.MessageTerminator)
				if err != nil {

					// If connection closed - break the cycle
					if errors.As(err, &io.ErrClosedPipe) {
						c.Logger.Info(fmt.Sprintf("(%v) - conn closed by other party", conn2.RemoteAddr()))
						return
					}
					c.Logger.Error(fmt.Sprintf("(%v)[ReadBytes] - error reading data: %v", conn2.RemoteAddr(), err))
					continue
				}

				err = message2.Parse(&netData2)
				if err != nil {
					c.Logger.Error(fmt.Sprintf("(%v)[message.Parse] - broken message: %v", conn2.RemoteAddr(), err))
					continue
				}

				if message2.Type == proto.MessageFileParts {
					n, err := theFile.Write(message2.Payload)
					if err != nil {
						fmt.Println("Errpr", err)
						return
					}

					fmt.Println("writen bytesL ", n)

					continue

				} else if message2.Type == proto.MessageFileEnd {

					fmt.Println("Got file end")

					err = os.Chtimes(tempPath, *updatedAt, *updatedAt)
					if err != nil {
						fmt.Println(err)
					}

					theFile.Close()

					// Move the file to right position
					err := os.Rename(tempPath, *fullPath)
					if err != nil {
						c.Logger.Error("moving failed: ", *fullPath, err)
					}

					dat, err := os.Stat(*fullPath)
					if err != nil {
						c.Logger.Error("Object reading failed: ", *fullPath, err)
						continue
					}

					// Add object to DB
					err = c.FW.MakeDBRecord(dat, *fullPath)
					if err != nil && err.Error() != "UNIQUE constraint failed: files.name, files.path" {
						c.Logger.Error(fmt.Sprintf("Error making record for %s: ", *fullPath), err)
					}
					break
				}
			}

			return
		} else {
			fmt.Println(message2.Type)
			continue
		}
	}
}
