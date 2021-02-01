package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

func discover() (string, string, string) {
	var dist string
	var version string

	var lsbDist string
	var distVersion string
	var distVersionMajor string
	var majorVersion string

	// Find the Linux Standard Distribution
	if _, err := os.Stat("/etc/centos-release"); err == nil {
		// this is for centos
		distCmd := fmt.Sprintf("%s", "cat /etc/os-centos-release | cut -d\" \" -f1")
		dist, _ = getBashData("/etc/os-centos-release", distCmd)

		// Get Version
		versionCmd := fmt.Sprintf("%s", "cat /etc/centos-release | sed 's/Linux //' | cut -d\" \" -f3 | cut -d \".\" -f1-2")
		version, _ = getBashData("/etc/os-centos-release", versionCmd)
	} else if _, err := os.Stat("/etc/redhat-release"); err == nil {
		// this is for rhel6
		dist = "rhel"

		// Get Version
		majorVersionCmd := fmt.Sprintf("%s", "cat /etc/redhat-release | cut -d\" \" -f7 | cut -d \".\" -f1")
		majorVersion, _ = getBashData("/etc/redhat-release", majorVersionCmd)
		version = majorVersion
	} else if _, err := os.Stat("/etc/os-release"); err == nil {
		// ubuntu
		// Get dist
		cmd := exec.Command("/bin/bash", "-c", "source /etc/os-release ; echo $ID;")
		output, err := cmd.CombinedOutput()
		if err == nil {
			dist = string(output)
		}
		// Get Version
		cmd = exec.Command("/bin/bash", "-c", "source /etc/os-release ; echo $VERSION_ID;")
		output, err = cmd.CombinedOutput()
		if err == nil {
			version = string(output)
		}
	}

	// Remove newlines if any
	dist = strings.TrimSuffix(dist, "\n")
	version = strings.TrimSuffix(version, "\n")

	// convert into lower
	dist = strings.ToLower(dist)
	if isValidDistribution(dist) == true {
		switch dist {
		case "ubuntu":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 12 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break
		case "debian":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 7 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break

		case "fedora":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 21 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break

		case "rhel":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 6 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break

		case "centos":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 6 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break

		case "amzn":
			if isValidAmznVersion(version) == true {
				lsbDist = dist
				distVersion = version
				distVersionMajor = version
			}
		case "sles":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 12 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break

		case "ol":
			major := strings.Split(version, ".")
			if v, _ := strconv.Atoi(major[0]); v > 6 {
				lsbDist = dist
				distVersion = version
				distVersionMajor = major[0]
			}
			break

		}
	}
	return lsbDist, distVersion, distVersionMajor
}

func isValidAmznVersion(version string) bool {

	amznVersionMap := map[string]bool{
		"2":       true,
		"2.0":     true,
		"2018.03": true,
		"2017.03": true,
		"2016.03": true,
		"2015.03": true,
		"2014.03": true,
		"2017.09": true,
		"2016.09": true,
		"2015.09": true,
		"2014.09": true,
	}

	if _, exists := amznVersionMap[version]; exists {
		return true
	}
	return false

}

func getBashData(fname string, bashCmd string) (string, error) {
	var output string
	if _, err := os.Stat(fname); err == nil {
		tmpFile, err := ioutil.TempFile(os.TempDir(), "kotsmetrics-")
		if err != nil {
			return output, errors.Wrap(err, "failed to create temp file")
		}
		defer os.Remove(tmpFile.Name())

		text := []byte(bashCmd)
		if _, err = tmpFile.Write(text); err != nil {
			return output, errors.Wrap(err, "failed to write to temp file")
		}
		os.Chmod(tmpFile.Name(), 0755)
		c, err := exec.Command("/bin/bash", tmpFile.Name()).Output()
		if err != nil {
			return output, errors.Wrap(err, "unable to execute bashCmd")
		}
		output = string(c)
		// Close the file
		if err := tmpFile.Close(); err != nil {
			return output, errors.Wrap(err, "unable to close file")
		}
	}
	return output, nil
}
func isValidDistribution(dist string) bool {
	distMap := map[string]bool{
		"ubuntu": true,
		"debian": true,
		"fedora": true,
		"rhel":   true,
		"centos": true,
		"amzn":   true,
		"sles":   true,
		"ol":     true,
	}
	if _, exists := distMap[dist]; exists {
		return true
	}
	return false
}
