package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/lazybark/pcloud-sync-server/users"
	"gorm.io/gorm"
)

type (
	// MessageType represents sync message types and human-readable name
	MessageType int

	// Message is the model for base sync message
	Message struct {
		Type    MessageType
		Payload []byte
	}

	Auth struct {
		Login    string
		Password string
	}

	Token struct {
		Timestamp time.Time
		Token     string
	}

	OK struct {
		OK            bool
		HumanReadable string
	}

	DirSyncReq struct {
		Token      string
		Root       string // Directory path that needs to be synced
		Filesystem Filesystem
	}

	DirSyncResp struct {
		Token          string
		Filesystem     Filesystem
		UploadToServer []string
	}

	Filesystem struct {
		Folders []Folder
		Files   []File
	}

	SyncFileData struct {
		Id            int
		Hash          string
		Name          string
		Path          string
		Size          int64
		FSUpdatedAt   time.Time
		CurrentStatus string
		LocationDirId int
		Type          string
		Data          []byte
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

// Message types
const (
	messages_start MessageType = iota

	MessageError
	MessageAuth          // Request for token by login & password
	MessageToken         // Response with newly generated token for client
	MessageDirSyncReq    // Request for filelist (client -> server) with own filelist in specific dir
	MessageDirSyncResp   // Response from server with filelist (server -> client) and list of files to upload on server in specific dir
	MessageGetFile       // Request to get []bytes of specific file (client -> server)
	MessageSendFile      // Response with []bytes of specific file (client <-> server)
	MessageConnectionEnd // Message to close connetion (client <-> server)
	MessageOK            // The other side correctly understood previous message OR not (client <-> server)

	messages_end
)

func (m MessageType) String() string {
	return [...]string{"", "Error", "Authorization", "New token", "MessageDirSyncReq", "MessageDirSyncResp", "MessageGetFile", "MessageSendFile", "MessageConnectionEnd", "OK", ""}[m]
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
		Timestamp:     time.Now(),
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
	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

func (m *Message) MakeToken(t string) error {
	payload := Token{
		Timestamp: time.Now(),
		Token:     t,
	}

	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(payload)
	if err != nil {
		return err
	}

	m.Type = MessageToken
	m.Payload = b.Bytes()

	return nil
}

func (m *Message) ReturnToken(c *net.Conn, token string) (bytesSent int, err error) {
	err = m.MakeToken(token)
	if err != nil {
		return
	}
	bytesSent, err = m.Send(c)
	if err != nil {
		return
	}

	return
}

func (m *Message) Send(c *net.Conn) (bytesSent int, err error) {
	response, err := json.Marshal(*m)
	if err != nil {
		return
	}

	bytesSent, err = io.WriteString(*c, string(response))
	if err != nil {
		return
	}

	return
}

func (m *Message) ProcessFullAuth(c *net.Conn, db *gorm.DB) (newToken string, err error) {
	// Parse payload
	auth, err := m.ValidateAuth()
	if err != nil {
		return
	}
	// Check credentials
	ok, userId, err := users.ValidateCreds(auth.Login, auth.Password, db)
	if err != nil {
		return
	}
	if !ok {
		return "", fmt.Errorf("wrong credentials")
	}
	// Token for the client
	newToken, err = users.GenerateToken()
	if err != nil {
		return
	}
	// Add token into DB
	err = users.RegisterToken(userId, newToken, db)
	if err != nil {
		return
	}

	return
}

func (m *Message) ValidateAuth() (auth *Auth, err error) {
	err = json.Unmarshal(m.Payload, &auth)
	if err != nil {
		return
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
		return
	}
	if ok == nil {
		err = fmt.Errorf("broken message")
		return
	}
	return
}
