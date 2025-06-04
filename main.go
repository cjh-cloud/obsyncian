// Obsyncian
package main

import (
	"errors"
	"fmt"
	"os"
    "encoding/json"
    "io"

    "github.com/google/uuid"
    "github.com/charmbracelet/huh"
)

// cmd to configure - AWS creds? s3 bucket to use, dir to use (optional)
// generate uuid for this user and save to config on first startup

// on startup, if configured, check dynamo, if another user last synced, sync from bucket?

func check(e error) {
    if e != nil {
        panic(e)
    }
}

func main() {
    // check config file exists in the home dir, and UUID exists
    // path := os.UserHomeDir() + "\\obsyncian\\config"
    home_dir, _ := os.UserHomeDir()
    path := fmt.Sprintf("%s/obsyncian/config.json", home_dir) // TODO : are we on windows? change slashes
    fmt.Printf("Path: '%s' \n", path)
    _, err := os.Stat(path)
	exists := !errors.Is(err, os.ErrNotExist)

    fmt.Printf("File exists: '%s' \n", exists)

    if (!exists){
        id := uuid.New()
        fmt.Printf("Creating new config for user %s", id)

        // create dir and file
        err := os.Mkdir(fmt.Sprintf("%s/obsyncian", home_dir), 0755)
        // check(err)

        f, err := os.Create(path)
        check(err)
        defer f.Close() // It’s idiomatic to defer a Close immediately after opening a file.
        n3, err := f.WriteString(fmt.Sprintf("{\n  \"id\" : \"%s\"\n}\n", id))
        check(err)
        fmt.Printf("wrote %d bytes\n", n3)
        f.Sync() // Issue a Sync to flush writes to stable storage.
    }

    // if it does exist, does the config have what we need? id, aws creds, s3 bucket, optional subdir in bucket, local dir to sync ?
    // read from file
    // Open our jsonFile
    jsonFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644) // os.Open(path)
    // if we os.Open returns an error then handle it
    if err != nil {
        fmt.Println(err)
    }
    fmt.Println("Successfully Opened %s", path)
    // defer the closing of our jsonFile so that we can parse it later on
    defer jsonFile.Close()

    type Credentials struct {
        Key string `json: "key"`
        Secret string `json: "secret"`
    }

    type Config struct {
        ID   string `json:"id"`
        Local   string `json:"local"`
        Cloud    string    `json:"cloud"` // cloud path, bucket?
        Provider string `json:"provider"` // only AWS allowed for starters
        Credentials Credentials `json:"credentials"`
    }

    // read our opened jsonFile as a byte array.
    byteValue, _ := io.ReadAll(jsonFile)

    // we initialize our Users array
    var config Config

    // we unmarshal our byteArray which contains our
    // jsonFile's content into 'users' which we defined above
    json.Unmarshal(byteValue, &config)

    fmt.Println("Config ID: " + config.ID)

    // TODO If values are missing, ask for user input

    if config.Local == "" {
        huh.NewInput().
            Title("What’s your local path?").
            Value(&config.Local).
            // Validating fields is easy. The form will mark erroneous fields
            // and display error messages accordingly.
            Validate(func(str string) error {
                if str == "Frank" {
                    return errors.New("Sorry, we don’t serve customers named Frank.")
                }
                return nil
            }).
            Run()
    }

    // Cloud
    if config.Cloud == "" {
        huh.NewInput().
            Title("What’s your cloud path (e.g. S3 bucket name)?").
            Value(&config.Cloud).
            // Validating fields is easy. The form will mark erroneous fields
            // and display error messages accordingly.
            Validate(func(str string) error {
                if str == "Frank" {
                    return errors.New("Sorry, we don’t serve customers named Frank.")
                }
                return nil
            }).
            Run()
    }

    // Provider
    if config.Provider == "" {
        huh.NewSelect[string]().
            Title("Choose your burger").
            Options(
                huh.NewOption("Amazon Web Services", "AWS"),
                huh.NewOption("Microsoft Azure", "Azure"),
                huh.NewOption("Google Cloud", "GCP"),
            ).
            Value(&config.Provider). // store the chosen option in the "burger" variable
            Run()
    }

    config_json, err := json.Marshal(config)
    check(err)
    configFileWriter, err := os.Create(path) // os.Open(path)
    _, err = configFileWriter.Write(config_json)
    // check(err)
    if err != nil {
        fmt.Println("Error writing to file:", err)
    }
    fmt.Println("Config file updated.")

    // err = form.Run()
    // if err != nil {
	// 	fmt.Println("Uh oh:", err)
	// 	os.Exit(1)
	// }

    // if !discount {
    //     fmt.Println("What? You didn’t take the discount?!")
    // }

    // TODO - we have our config file, we can now start reading from Dynamo, and syncing with S3

    // If cloud has new changes, pull them
    // If I have new changes, and am up to date with cloud, push them

    // read from Dynamo table
    // if Dynamo table empty -> create first entry, and sync from local to cloud
    // if Dynamo has entry
    //    if last uuid is us, sync up, and update time changed
    //    if last uuid is not us, sync down dry run, if changes sync down and store latest time in memory
    // dry run sync up, if changes, and latest sync in dynamo is not newer than what is stored in memeory, sync up

}