package sync

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lazybark/go-helpers/hasher"
	"github.com/lazybark/pcloud-sync-server/fsworker"
	"github.com/lazybark/pcloud-sync-server/users"
	"gorm.io/gorm"
)

type (
	// MessageType represents sync message types and human-readable name
	MessageType int

	SyncEvent int

	SyncObject int

	// Message is the model for base sync message
	Message struct {
		Type      MessageType
		Token     string
		Timestamp time.Time
		Payload   []byte
	}

	Handshake struct {
		PartyName             string
		AppVersion            string
		OwnerContacts         string
		MaxClients            int
		MaxConnectionsPerUser int
		MaxFileSize           int
	}

	SyncEventPayload struct {
		Type         SyncEvent  // What happened: created, deleted, updated
		ObjectType   SyncObject // What object involved: dir or file
		Name         string
		Path         string
		Hash         string
		NewUpdatedAt time.Time
	}

	Auth struct {
		Login      string
		Password   string
		DeviceName string
		Label      string
		RestrictIP bool
	}

	Token struct {
		Token string
	}

	OK struct {
		OK            bool
		HumanReadable string
	}

	DirSyncReq struct {
		Token      string
		Root       string // Directory path that needs to be synced
		Filesystem fsworker.Filesystem
	}

	DirSyncResp struct {
		Token          string
		Filesystem     fsworker.Filesystem
		UploadToServer []string
	}

	SyncFileData struct {
		Hash        string
		Name        string
		Path        string
		Size        int64
		FSUpdatedAt time.Time
		Type        string
		Data        []byte
	}

	SyncDirData struct {
		Id            int
		Name          string
		Path          string
		CurrentStatus string
		LocationDirId int
		Data          []byte
	}
)

const (
	sync_events_start SyncEvent = iota

	ObjectCreated
	ObjectUpdated
	ObjectRemoved

	sync_events_end
	SyncEventIllegal // Just for readability
)

func (e SyncEvent) String() string {
	if sync_events_start < e && e < sync_events_end {
		return "illegal SyncEvent"
	}
	return [...]string{"illegal SyncEvent", "Created", "Updated", "Deleted", "illegal SyncEvent", "illegal SyncEvent"}[e]
}

func SyncEventFromWatcherEvent(watcherEvent fsnotify.Op) SyncEvent {
	if watcherEvent == fsnotify.Create {
		return ObjectCreated
	} else if watcherEvent == fsnotify.Remove || watcherEvent == fsnotify.Rename {
		return ObjectRemoved
	} else if watcherEvent == fsnotify.Write {
		return ObjectUpdated
	}

	// Return this kind just for better code readability
	return SyncEventIllegal
}

func (ep *SyncEventPayload) CheckType() bool {
	if sync_events_start < ep.Type && ep.Type < sync_events_end {
		return true
	}
	return false
}

const (
	sync_objects_start SyncObject = iota

	ObjectDir
	ObjectFile

	sync_objects_end
)

func (o SyncObject) String() string {
	if sync_objects_start < o && o < sync_objects_end {
		return "illegal SyncObject"
	}
	return [...]string{"illegal SyncObject", "Directory", "File", "illegal SyncObject"}[o]
}

func (e *SyncEventPayload) CheckObject() bool {
	if sync_objects_start < e.ObjectType && e.ObjectType < sync_objects_end {
		return true
	}
	return false
}

// Message types
const (
	messages_start MessageType = iota

	MessageError
	MessageAuth            // Request for token by login & password
	MessageToken           // Response with newly generated token for client
	MessageDirSyncReq      // Request for filelist (client -> server) with own filelist in specific dir
	MessageDirSyncResp     // Response from server with filelist (server -> client) and list of files to upload on server in specific dir
	MessageGetFile         // Request to get []bytes of specific file (client -> server)
	MessageSendFile        // Response with []bytes of specific file (client <-> server)
	MessageConnectionEnd   // Message to close connetion (client <-> server)
	MessageOK              // The other side correctly understood previous message OR not (client <-> server)
	MessageStartSync       // The other party is ready to recieve filesystem events
	MessageEndSync         // The other side doesn't need filesystem events anymore
	MessageCloseConnection // The other side doesn't need the connection anymore **POSSIBLY REDUNDANT**
	MessageSyncEvent       // Notify other perties that file or dir were created / changed / deleted
	MessageHandshake       // Notify other perties that file or dir were created / changed / deleted

	messages_end
)

func (m MessageType) String() string {
	return [...]string{"illegal", "Error", "Authorization", "New token", "MessageDirSyncReq", "MessageDirSyncResp", "MessageGetFile", "MessageSendFile", "MessageConnectionEnd", "OK", "MessageStartSync", "MessageEndSync", "MessageCloseConnection", "MessageSyncEvent", "illegal"}[m]
}

func (m *Message) CheckType() bool {
	if messages_start < m.Type && m.Type < messages_end {
		return true
	}
	return false
}

/*
func (m *Message) Read(c net.Conn) (recievedBytes int, err error) {
	buf := make([]byte, 256)
	var n int
	var recievedData []byte

	// Read message contents
	for {
		n, err = c.Read(buf)
		recievedBytes += n
		recievedData = append(recievedData, buf[:n]...)

		if err == io.EOF {
			err = nil
			break
		} else if err != nil {
			return
		}
	}

	// Decoding base message to retrieve its type
	err = m.Parse(&recievedData)
	if err != nil {
		return
	}

	return
}*/

// Parse decodes incoming byte slice into sync.Message struct
func (m *Message) Parse(bytes *[]byte) error {
	err := json.Unmarshal(*bytes, &m)
	if err != nil {
		return err
	}

	if ok := m.CheckType(); !ok {
		return fmt.Errorf("unknown message type")
	}

	return nil
}

func (m *Message) MakeError(e ErrorType) error {
	if ok := e.CheckErrorType(); !ok {
		return fmt.Errorf("incorrect err type")
	}
	errorPayload := Error{
		Type:          e,
		HumanReadable: e.String(),
	}

	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(errorPayload)
	if err != nil {
		return err
	}

	m.Type = MessageError
	m.Payload = b.Bytes()

	return nil
}

func (m *Message) ReturnError(c *net.Conn, e ErrorType) (bytesSent int, err error) {
	err = m.MakeError(e)
	if err != nil {
		return
	}

	m.Timestamp = time.Now()

	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

func (m *Message) MakeToken(t string) error {
	payload := Token{
		Token: t,
	}

	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(payload)
	if err != nil {
		return err
	}

	m.Payload = b.Bytes()

	return nil
}

func (m *Message) ReturnToken(c *net.Conn, token string) (bytesSent int, err error) {
	err = m.MakeToken(token)
	if err != nil {
		return
	}

	m.Type = MessageToken
	m.Timestamp = time.Now()

	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

func (m *Message) ParseSyncEvent() (p SyncEventPayload, err error) {
	err = json.Unmarshal(m.Payload, &p)
	if err != nil {
		return p, fmt.Errorf("[ParseSyncEvent] error unmarshalling -> %w", err)
	}
	if p.Name == "" {
		err = fmt.Errorf("no object found")
		return
	}

	if !p.CheckType() {
		err = fmt.Errorf("unknown event type")
		return
	}

	if !p.CheckObject() {
		err = fmt.Errorf("unknown object type")
		return
	}
	return
}

func (m *Message) SendGetFile(c interface{}, file GetFile) (bytesSent int, err error) {
	m.Type = MessageGetFile

	payload := GetFile{
		Name:      file.Name,
		Path:      file.Path,
		Hash:      file.Hash,
		UpdatedAt: file.UpdatedAt,
	}

	b := new(bytes.Buffer)
	err = json.NewEncoder(b).Encode(payload)
	if err != nil {
		return
	}

	m.Payload = b.Bytes()
	m.Timestamp = time.Now()

	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

func (m *Message) ParseGetFile() (getFile GetFile, err error) {
	err = json.Unmarshal(m.Payload, &getFile)
	if err != nil {
		return getFile, fmt.Errorf("[ParseGetFile] error unmarshalling -> %w", err)
	}
	// We need to understad what type of error this is - 'unknown' is not an option
	if getFile.Name == "" {
		err = fmt.Errorf("broken message")
		return
	}
	return
}

func (m *Message) SendFile(c interface{}, file string, fw *fsworker.Fsworker) (bytesSent int, err error) {
	m.Type = MessageSendFile
	var payload SyncFileData

	stat, err := os.Stat(file)
	if err != nil {
		return
	}
	fmt.Println("DDD1")

	fileData, err := os.Open(file)
	if err != nil {
		return 0, fmt.Errorf("can not open file -> %w", err)
	}
	defer fileData.Close()
	fmt.Println("DDD2")

	dir, _ := filepath.Split(file)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))

	hash, err := hasher.HashFilePath(file, hasher.SHA256, 8192)
	if err != nil {
		return
	}
	fmt.Println("DDD3")

	payload = SyncFileData{
		Name:        filepath.Base(file),
		Path:        fw.EscapeAddress(dir),
		Hash:        hash,
		Size:        stat.Size(),
		FSUpdatedAt: stat.ModTime(),
		Type:        filepath.Ext(file),
	}

	//data := []byte{}
	buf := make([]byte, 8192)
	n := 0

	r := bufio.NewReader(fileData)

	for {
		n, err = r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("ERRRE", err)
			return
		}

		payload.Data = append(payload.Data, buf[:n]...)

	}
	fmt.Println("DDD4")

	b := new(bytes.Buffer)
	err = json.NewEncoder(b).Encode(payload)
	if err != nil {
		return
	}

	m.Payload = b.Bytes()
	m.Timestamp = time.Now()

	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}
	fmt.Println("DDD5")

	return
}

func (m *Message) ParseFileData() (data SyncFileData, err error) {
	err = json.Unmarshal(m.Payload, &data)
	if err != nil {
		return data, fmt.Errorf("[ParseFileData] error unmarshalling -> %w", err)
	}
	// We need to understad what type of error this is - 'unknown' is not an option
	if data.Name == "" {
		err = fmt.Errorf("broken message")
		return
	}
	return

}

func (m *Message) SendSyncEvent(c interface{}, eventType fsnotify.Op, o interface{}) (bytesSent int, err error) {
	m.Type = MessageSyncEvent
	var payload SyncEventPayload

	file, okf := o.(fsworker.File)
	if okf {
		fmt.Println("sdsd")
		payload = SyncEventPayload{
			Type:         SyncEventFromWatcherEvent(eventType),
			ObjectType:   ObjectFile,
			Name:         file.Name,
			Path:         file.Path,
			Hash:         file.Hash,
			NewUpdatedAt: file.FSUpdatedAt,
		}
	}

	folder, okd := o.(fsworker.Folder)
	if okd {
		payload = SyncEventPayload{
			Type:         SyncEventFromWatcherEvent(eventType),
			ObjectType:   ObjectDir,
			Name:         folder.Name,
			Path:         folder.Path,
			Hash:         "",
			NewUpdatedAt: folder.FSUpdatedAt,
		}
	}

	if !okf && !okd {
		return bytesSent, errors.New("[SendSyncEvent] o is not suitable object")
	}

	b := new(bytes.Buffer)
	err = json.NewEncoder(b).Encode(payload)
	if err != nil {
		return
	}

	m.Payload = b.Bytes()
	m.Timestamp = time.Now()

	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

func (m *Message) SendAuthMessage(c interface{}, login string, password string, deviceName string, label string, restrictIP bool) (bytesSent int, err error) {
	m.Type = MessageAuth

	payload := Auth{
		Login:      login,
		Password:   password,
		DeviceName: deviceName,
		Label:      label,
		RestrictIP: restrictIP,
	}

	b := new(bytes.Buffer)
	err = json.NewEncoder(b).Encode(payload)
	if err != nil {
		return
	}

	m.Payload = b.Bytes()
	m.Timestamp = time.Now()

	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

// ReturnInfoMessage sends message with any MessageType specified and empty Payload field
func (m *Message) ReturnInfoMessage(c interface{}, t MessageType) (bytesSent int, err error) {

	m.Type = t

	ok := m.CheckType()
	if !ok {
		return bytesSent, fmt.Errorf("[ReturnInfoMessage] wrong MessageType")
	}

	bytesSent, err = m.Send(c)
	if err != nil {
		return bytesSent, fmt.Errorf("[ReturnInfoMessage] error sending message -> %v", err)
	}

	return
}

func (m *Message) Send(c interface{}) (bytesSent int, err error) {
	response, err := json.Marshal(*m)
	if err != nil {
		return
	}

	nc, ok := c.(*net.Conn)
	if ok {
		bytesSent, err = io.WriteString(*nc, string(response)+MessageTerminatorString)
		if err != nil {
			return bytesSent, fmt.Errorf("[m.Send] (nc) error writing response -> %v", err)
		}
		return
	}

	tc, ok := c.(*tls.Conn)
	if ok {
		bytesSent, err = io.WriteString(tc, string(response)+MessageTerminatorString)
		if err != nil {
			return bytesSent, fmt.Errorf("[m.Send] (tc) error writing response -> %v", err)
		}
		return
	}

	if !ok {
		return 0, fmt.Errorf("[m.Send] c is not a suitable connection")
	}

	return
}

func (m *Message) ProcessFullAuth(c *net.Conn, db *gorm.DB, tokenValidDays int) (newToken string, err error) {
	// Parse payload
	auth, err := m.ValidateAuth()
	if err != nil {
		return newToken, fmt.Errorf("[ValidateAuth] error validating -> %w", err)
	}
	// Check credentials
	ok, userId, err := users.ValidateCreds(auth.Login, auth.Password, db)
	if err != nil {
		return newToken, fmt.Errorf("[ValidateCreds] error validating -> %w", err)
	}
	if !ok {
		return newToken, fmt.Errorf("wrong credentials")
	}
	// Token for the client
	newToken, err = users.GenerateToken()
	if err != nil {
		return newToken, fmt.Errorf("[GenerateToken] error generating -> %w", err)
	}
	// Add token into DB
	err = users.RegisterToken(userId, newToken, db, tokenValidDays)
	if err != nil {
		return newToken, fmt.Errorf("[RegisterToken] error registering -> %w", err)
	}

	return
}

func (m *Message) ParseNewToken() (newToken Token, err error) {
	err = json.Unmarshal(m.Payload, &newToken)
	if err != nil {
		return newToken, fmt.Errorf("[ParseNewToken] error unmarshalling -> %w", err)
	}
	if newToken.Token == "" {
		err = fmt.Errorf("no token found")
		return
	}
	return
}

func (m *Message) ParseError() (errServer Error, err error) {
	err = json.Unmarshal(m.Payload, &errServer)
	if err != nil {
		return errServer, fmt.Errorf("[ParseError] error unmarshalling -> %w", err)
	}
	// We need to understad what type of error this is - 'unknown' is not an option
	if !errServer.Type.CheckErrorType() {
		err = fmt.Errorf("error unknown")
		return
	}
	return
}

func (m *Message) ValidateAuth() (auth *Auth, err error) {
	err = json.Unmarshal(m.Payload, &auth)
	if err != nil {
		return nil, fmt.Errorf("[json.Unmarshal] error unmarshalling -> %w", err)
	}
	if auth == nil {
		err = fmt.Errorf("broken message")
		return
	}
	return
}

func (m *Message) ValidateOK() (ok *OK, err error) {
	err = json.Unmarshal(m.Payload, &ok)
	if err != nil {
		return nil, fmt.Errorf("[json.Unmarshal] error unmarshalling -> %w", err)
	}
	if ok == nil {
		err = fmt.Errorf("broken message")
		return
	}
	return
}

/*
func (m *Message) ValidateInfo() (info *Info, err error) {
	err = json.Unmarshal(m.Payload, &info)
	if err != nil {
		return nil, fmt.Errorf("[json.Unmarshal] error unmarshalling -> %w", err)
	}
	if info == nil {
		err = fmt.Errorf("broken message")
		return
	}
	return
}*/
