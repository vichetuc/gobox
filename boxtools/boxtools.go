package boxtools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/golangbox/gobox/server/model"
	"github.com/golangbox/gobox/structs"
	"github.com/jinzhu/gorm"

	"code.google.com/p/go.crypto/bcrypt"
)

var salt []byte

func init() {
	f, err := os.Open("/dev/random")
	if err != nil {
		panic("Unable to open /dev/random")
	}
	salt = make([]byte, 8)
	n, err := f.Read(salt)
	if n != 8 || err != nil {
		panic("Couldn't read from dev/random")
	}
	f.Close()

	rand.Seed(time.Now().Unix())

}

func RandomString(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += string('a' + rand.Intn(26))
	}
	return s
}

func GenerateFilePathFromRoot(root string) string {
	depth := 3
	s := root
	for i := 0; i < depth; i++ {

		s += string(filepath.Separator)
		s += RandomString(rand.Intn(10) + 1)
	}
	return s
}

func GenerateRandomFile(user_id int) (file structs.File, err error) {
	path := GenerateFilePathFromRoot("/path")
	basename := filepath.Base(path)
	h, err := GenerateRandomSha256()
	if err != nil {
		return file, err
	}
	return structs.File{
		UserId:    int64(user_id),
		Name:      basename,
		Hash:      h,
		Size:      rand.Int63(),
		Modified:  time.Now(),
		Path:      path,
		CreatedAt: time.Now(),
	}, err

}

func GenerateRandomFileAction(client_id int, user_id int, isCreate bool) (fileAction structs.FileAction, err error) {
	file, err := GenerateRandomFile(user_id)
	if err != nil {
		return fileAction, err
	}
	return structs.FileAction{
		ClientId:  int64(client_id),
		IsCreate:  isCreate,
		CreatedAt: time.Now(),
		File:      file,
	}, err
}

func GenerateSliceOfRandomFileActions(user_id int, clients int, actions int) (fileActions []structs.FileAction, err error) {
	fileActions = make([]structs.FileAction, actions)
	for i := 0; i < int(actions); i++ {
		isAction := rand.Intn(2) == 1
		action, err := GenerateRandomFileAction(rand.Intn(clients)+1, user_id, isAction)
		if err != nil {
			return fileActions, err
		}
		fileActions[i] = action
	}
	return fileActions, err
}

func GenerateNoisyAndNonNoisyFileActions(user_id int, clients int, totalNonNoisyActions int, createPairs int) (nonNoisyActions []structs.FileAction,
	noisyActions []structs.FileAction, err error) {
	numNoisyActions := totalNonNoisyActions + createPairs
	nonNoisyActions, err = GenerateSliceOfRandomFileActions(user_id, clients, totalNonNoisyActions)
	if err != nil {
		return
	}
	noisyActions = make([]structs.FileAction, numNoisyActions)
	copy(noisyActions, nonNoisyActions)
	offset := len(nonNoisyActions)
	for i := 0; i < createPairs; i++ {
		new := nonNoisyActions[i]
		new.IsCreate = !new.IsCreate
		noisyActions[i+offset] = new
	}
	return

}

func NewUser(email string, password string) (user structs.User, err error) {
	hash, err := hashPassword(password)
	if err != nil {
		return
	}
	user = structs.User{
		Email:          email,
		HashedPassword: hash,
	}
	query := model.DB.Create(&user)
	if query.Error != nil {
		return user, query.Error
	}
	client, err := NewClient(user, "Server", true)
	_ = client
	if err != nil {
		return
	}
	return
}

func NewClient(user structs.User, name string, isServer bool) (client structs.Client, err error) {
	// calculate key if we need a key?
	newKey, err := GenerateRandomSha256()
	if err != nil {
		return
	}
	client = structs.Client{
		UserId:     user.Id,
		SessionKey: newKey,
		IsServer:   isServer,
		Name:       name,
	}
	query := model.DB.Create(&client)
	if query.Error != nil {
		return client, query.Error
	}
	return
}

func GenerateRandomSha256() (s string, err error) {
	h := sha256.New()
	h.Write(salt)
	io.WriteString(h, time.Now().String())
	bytes := h.Sum(nil)
	s = hex.EncodeToString(bytes)
	return s, err
}

func ValidateUserPassword(email, password string) (user structs.User, err error) {
	model.DB.Where("email = ?", email).First(&user)
	bytePassword := []byte(password)
	byteHash := []byte(user.HashedPassword)
	err = bcrypt.CompareHashAndPassword(byteHash, bytePassword)
	return user, err
}

func clear(b []byte) {
	for i := 0; i < len(b); i++ {
		b[i] = 0
	}
}

func hashPassword(password string) (hash string, err error) {
	bytePassword := []byte(password)
	defer clear(bytePassword)
	byteHash, err := bcrypt.GenerateFromPassword(bytePassword, bcrypt.DefaultCost)
	return string(byteHash), err
}

func ConvertJsonStringToFileActionsStruct(jsonFileAction string, client structs.Client) (fileAction structs.FileAction, err error) {
	// {"Id":0,"ClientId":0,"IsCreate":true,"CreatedAt":"0001-01-01T00:00:00Z","File":{"Id":0,"UserId":0,"Name":"client.go","Hash":"f953d35b6d8067bf2bd9c46017c554b73aa28a549fac06ba747d673b2da5bfe0","Size":6622,"Modified":"2015-02-09T14:39:22-05:00","Path":"./client.go","CreatedAt":"0001-01-01T00:00:00Z"}}
	data := []byte(jsonFileAction)
	var unmarshalledFileAction structs.FileAction
	err = json.Unmarshal(data, &unmarshalledFileAction)
	if err != nil {
		err = fmt.Errorf("Error unmarshaling json to structs.FileAction: %s", err)
		return
	}
	return
}

func ConvertFileActionStructToJsonString(fileActionStruct structs.FileAction) (fileActionJson string, err error) {
	jsonBytes, err := json.Marshal(fileActionStruct)
	if err != nil {
		return
	}
	fileActionJson = string(jsonBytes)
	return
}

func RemoveRedundancyFromFileActions(fileActions []structs.FileAction) (simplifiedFileActions []structs.FileAction) {
	// removing redundancy
	// if a file is created, and then deleted remove

	var actionMap = make(map[string]int)

	var isCreateMap = make(map[bool]string)
	isCreateMap[true] = "1"
	isCreateMap[false] = "0"

	for _, action := range fileActions {
		// create a map of the fileaction
		// key values are task+path+hash
		// hash map value is the number of occurences
		actionKey := isCreateMap[action.IsCreate] + action.File.Path + action.File.Hash
		value, exists := actionMap[actionKey]
		if exists {
			actionMap[actionKey] = value + 1
		} else {
			actionMap[actionKey] = 1
		}
	}

	for _, action := range fileActions {
		// for each action value, check if a pair exists in the map
		// if it does remove one iteration of that value
		// if it doesn't write that to the simplified array

		// This ends up removing pairs twice, once for each matching value
		var opposingTask string
		if action.IsCreate == true {
			opposingTask = isCreateMap[false]
		} else {
			opposingTask = isCreateMap[true]
		}
		opposingMapKey := opposingTask + action.File.Path + action.File.Hash
		value, exists := actionMap[opposingMapKey]
		if exists == true && value > 0 {
			actionMap[opposingMapKey] = value - 1
		} else {
			simplifiedFileActions = append(simplifiedFileActions, action)
		}
	}
	return
}

func ComputeFilesFromFileActions(fileActions []structs.FileAction) (files []structs.File) {
	simplifiedFileActions := RemoveRedundancyFromFileActions(fileActions)
	for _, value := range simplifiedFileActions {
		files = append(files, value.File)
	}
	return
}

func WriteFileActionsToDatabase(fileActions []structs.FileAction, client structs.Client) (err error) {
	var user structs.User
	query := model.DB.Model(&client).Related(&user)
	if query.Error != nil {
		err = query.Error
		return
	}
	for _, fileAction := range fileActions {
		file, err := FindFile(fileAction.File.Hash, fileAction.File.Path, user)
		if err != nil {
			return err
		}
		if file.Id != 0 {
			// if the file exists, assign an id
			// otherwise GORM automatically creates
			// the file, so make sure to clear the File
			// struct
			fileAction.FileId = file.Id
			fileAction.File = structs.File{}
		}
		query = model.DB.Create(&fileAction)
		if query.Error != nil {
			return query.Error
		}
	}
	return
}

func FindFile(hash string, path string, user structs.User) (file structs.File, err error) {
	query := model.DB.Where(&structs.File{
		UserId: user.Id,
		Path:   path,
		Hash:   hash,
	}).First(&file)
	if query.Error != nil {
		if query.Error != gorm.RecordNotFound {
			return file, query.Error
		} else {
			return
		}
	}
	return //this should never happen
}

func ApplyFileActionsToFilesTable(fileActions []structs.FileAction, user structs.User) (err error) {
	// for _, fileAction := range fileActions {
	// 	if fileAction.IsCreate == true {
	// 		// what if the path is the same?
	// 		model.DB.Create(&fileAction.File)
	// 	} else {
	// 		var file structs.File
	// 		query := model.DB.Where("path = ?", fileAction.File.Path).First(&file)
	// 		if query.Error != nil {
	// 			// uh oh
	// 		}
	// 		if file.Hash != fileAction.File.Hash {
	// 			// uh oh
	// 		}
	// 		model.DB.Delete(&file)
	// 	}
	// }
	return
}
