package users

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/lazybark/go-pretty-code/console"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"gorm.io/gorm"
)

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) (ok bool, err error) {
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil, err
}

func ValidateCreds(login string, password string, db *gorm.DB) (ok bool, userId uint, err error) {
	var user User
	if err = db.Where("login = ?", login).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
		return
	}

	ok, err = CheckPasswordHash(password, user.PasswordHash)
	if err != nil {
		return
	}

	userId = user.ID

	return
}

func ValidateToken(token string, db *gorm.DB) (ok bool, err error) {
	var client Client

	db.Select("clients.token_issued_at, clients.token_expires_at").Where("token = ? and users.role > 0", token).Joins("left join users on clients.user_id = users.id").First(&client)
	//data := fgfdg.Row()

	//data.Scan()

	/*if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	} else {
		return
	}*/
	fmt.Println("client")
	fmt.Println(client)
	fmt.Println("err")
	fmt.Println(err)

	if time.Now().Unix() >= client.TokenExpiresAt.Unix() || time.Now().Unix() < client.TokenIssuedAt.Unix() {
		return false, errors.New("token expired")
	}

	return true, nil
}

func CreateUserCLI(db *gorm.DB) (id uint, err error) {
	//get personal data from CLI
	scanner := bufio.NewScanner(os.Stdin)
	//name
	fmt.Println("Provide first user data")
	fmt.Print("Name -> ")
	scanner.Scan()
	name := scanner.Text()
	//lastname
	fmt.Print("Lastname -> ")
	scanner.Scan()
	lastname := scanner.Text()
	//email
	fmt.Print("Email -> ")
	scanner.Scan()
	email := scanner.Text()
	//login
	fmt.Print("Login -> ")
	scanner.Scan()
	login := scanner.Text()
	//password
	fmt.Print("Password (text will not appear on screen) -> ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	passwordStr := string(password)
	hash, err := HashPassword(passwordStr)
	if err != nil {
		return
	}
	fmt.Print("\nRole (1 - Blocked, 2 - Regular, " + console.ForeYellow("3 - Admin") + ", " + console.ForeRed("4 - Super") + ") -> ")
	var roleUser UserRole
	for {
		scanner.Scan()
		role := scanner.Text()
		roleInt, _ := strconv.Atoi(role)

		correct := roleUser.AssignRole(roleInt)
		if !correct {
			fmt.Print(console.ForeRed("Incorrect role.")+" Only ", roles_beg.Int()+1, " - ", roles_end.Int()-1, " allowed -> ")
		} else {
			break
		}

	}
	/*if err != nil {
		fmt.Println(err)
	}*/

	firstUser := User{
		Name:         name,
		LastName:     lastname,
		Email:        email,
		Login:        login,
		PasswordHash: hash,
		Role:         roleUser,
	}
	err = db.Save(&firstUser).Error
	id = firstUser.ID
	return
}

func GenerateToken() (token string, err error) {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_*+-?!")

	rand.Seed(time.Now().UnixNano())

	b := make([]rune, 64)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	token = string(b)

	return
}

func RegisterToken(userId uint, token string, db *gorm.DB, tokenValidDays int) (err error) {
	newClient := Client{
		UserId:         uint(userId),
		Token:          token,
		TokenIssuedAt:  time.Now().AddDate(0, 0, tokenValidDays),
		TokenExpiresAt: time.Now(),
		FirstConnectAt: time.Now(),
		LastConnectAt:  time.Now(),
	}
	err = db.Save(&newClient).Error

	return
}
