package main

import (
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
)

///////////////////////////////////////////////////////////////////////////////

type applicationConfiguration struct {
	Executable string

	SourceUrl      string
	SourceLogin    string
	SourcePassword string
	SourceInclude  string

	DestinationUrl      string
	DestinationLogin    string
	DestinationPassword string
}

type registryCatalog struct {
	Repositories []string `json:"repositories"`
}

type registryTags struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

///////////////////////////////////////////////////////////////////////////////

var (
	config applicationConfiguration
)

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)

	viper.SetConfigName("application")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("unable to read config file: %s", err)
	}
	config = applicationConfiguration{
		Executable:    "docker",
		SourceInclude: ".*",
	}
	if err := viper.UnmarshalKey("app", &config); err != nil {
		log.Errorf("unable to read application config: %s", err)
	}
}

func main() {
	if err := login(config.SourceUrl, config.SourceLogin, config.SourcePassword); err != nil {
		log.Fatalf("unable to login to source registry: %s", err)
	}
	if err := login(config.DestinationUrl, config.DestinationLogin, config.DestinationPassword); err != nil {
		log.Fatalf("unable to login to destination registry: %s", err)
	}

	catalog, err := catalog(config.SourceUrl, config.SourceLogin, config.SourcePassword)
	if err != nil {
		log.Fatalf("unable to get source catalog: %s", err)
	}

	regex, err := regexp.Compile(config.SourceInclude)
	if err != nil {
		log.Fatalf("unable to compile include regex: %s", err)
	}

	for _, image := range catalog {
		if !regex.MatchString(image) {
			continue
		}

		tags, err := tags(config.SourceUrl, config.SourceLogin, config.SourcePassword, image)
		if err != nil {
			log.Errorf("unable to get tags for %s: %s", image, err)
		}

		fmt.Printf("tags for %s: %s\n", image, tags)

		for _, tag := range tags {
			if err := copyImageTag(config.SourceUrl, config.DestinationUrl, image, tag); err != nil {
				log.Errorf("unable to copy %s:%s", image, tag)
			}
		}
	}
}

func login(url, login, password string) error {
	cmd := exec.Command(config.Executable, "login", "--username", login, "--password", password, url)
	return cmd.Run()
}

func catalog(url, login, password string) ([]string, error) {
	queryUrl := fmt.Sprintf("https://%s:%s@%s/v2/_catalog", login, password, url)

	response, err := http.Get(queryUrl)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("catalog %s unexpected status: %s", url, response.Status))
	}

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var catalog registryCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}

	return catalog.Repositories, nil
}

func tags(url, login, password, image string) ([]string, error) {
	queryUrl := fmt.Sprintf("https://%s:%s@%s/v2/%s/tags/list", login, password, url, image)

	response, err := http.Get(queryUrl)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("tags %s unexpected status: %s", image, response.Status))
	}

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var tags registryTags
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, err
	}

	return tags.Tags, nil
}

func copyImageTag(sourceUrl, destinationUrl, image, tag string) error {
	sourceImageName := fmt.Sprintf("%s/%s:%s", sourceUrl, image, tag)
	destinationImageName := fmt.Sprintf("%s/%s:%s", destinationUrl, image, tag)

	log.Infof("pull %s", sourceImageName)
	cmd := exec.Command(config.Executable, "image", "pull", "-q", sourceImageName)
	err := cmd.Run()
	if err != nil {
		return err
	}

	log.Infof("tag %s to %s", sourceImageName, destinationImageName)
	cmd = exec.Command(config.Executable, "image", "tag", sourceImageName, destinationImageName)
	err = cmd.Run()
	if err != nil {
		return err
	}

	log.Infof("push %s", destinationImageName)
	cmd = exec.Command(config.Executable, "image", "push", destinationImageName)
	err = cmd.Run()
	if err != nil {
		return err
	}

	log.Infof("remove %s and %s", sourceImageName, destinationImageName)
	cmd = exec.Command(config.Executable, "image", "rm", sourceImageName, destinationImageName)
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
