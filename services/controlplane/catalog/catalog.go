package catalog

import (
	"fmt"
	"strings"

	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)



var officialImages = []domain.OfficialImage{
	{
		Name:         "ubuntu-24.04",
		OS:           "ubuntu",
		OSVersion:    "24.04",
		Architecture: "amd64",
		URL:          "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
		Description:  "Ubuntu 24.04 LTS (Noble) cloud image - amd64",
		SizeBytes:    0,
	},
	{
		Name:         "ubuntu-22.04",
		OS:           "ubuntu",
		OSVersion:    "22.04",
		Architecture: "amd64",
		URL:          "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img",
		Description:  "Ubuntu 22.04 LTS (Jammy) cloud image - amd64",
		SizeBytes:    0,
	},
	{
		Name:         "ubuntu-20.04",
		OS:           "ubuntu",
		OSVersion:    "20.04",
		Architecture: "amd64",
		URL:          "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img",
		Description:  "Ubuntu 20.04 LTS (Focal) cloud image - amd64",
		SizeBytes:    0,
	},
	{
		Name:         "ubuntu-24.04-arm64",
		OS:           "ubuntu",
		OSVersion:    "24.04",
		Architecture: "arm64",
		URL:          "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img",
		Description:  "Ubuntu 24.04 LTS (Noble) cloud image - arm64",
		SizeBytes:    0,
	},
	{
		Name:         "debian-12",
		OS:           "debian",
		OSVersion:    "12",
		Architecture: "amd64",
		URL:          "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-amd64.qcow2",
		Description:  "Debian 12 (Bookworm) generic cloud image - amd64",
		SizeBytes:    0,
	},
	{
		Name:         "debian-11",
		OS:           "debian",
		OSVersion:    "11",
		Architecture: "amd64",
		URL:          "https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-generic-amd64.qcow2",
		Description:  "Debian 11 (Bullseye) generic cloud image - amd64",
		SizeBytes:    0,
	},
	{
		Name:         "debian-12-arm64",
		OS:           "debian",
		OSVersion:    "12",
		Architecture: "arm64",
		URL:          "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-arm64.qcow2",
		Description:  "Debian 12 (Bookworm) generic cloud image - arm64",
		SizeBytes:    0,
	},
}


var aliases = map[string]string{
	"ubuntu":       "ubuntu-22.04",
	"ubuntu-lts":   "ubuntu-22.04",
	"ubuntu-latest": "ubuntu-24.04",
	"debian":       "debian-12",
	"debian-latest": "debian-12",
}


func List() []domain.OfficialImage {
	out := make([]domain.OfficialImage, len(officialImages))
	copy(out, officialImages)
	return out
}




func Find(os, version, arch string) (*domain.OfficialImage, error) {
	os = strings.ToLower(strings.TrimSpace(os))
	version = strings.TrimSpace(version)
	arch = strings.ToLower(strings.TrimSpace(arch))
	if arch == "" {
		arch = "amd64"
	}


	if alias, ok := aliases[os]; ok && version == "" {

		for i := range officialImages {
			if officialImages[i].Name == alias && officialImages[i].Architecture == arch {
				return &officialImages[i], nil
			}
		}
	}

	combined := os
	if version != "" {
		combined = fmt.Sprintf("%s-%s", os, version)
		if arch != "amd64" {
			combined = fmt.Sprintf("%s-%s-%s", os, version, arch)
		}
	}
	for i := range officialImages {
		if officialImages[i].Name == combined {
			return &officialImages[i], nil
		}
	}

	for i := range officialImages {
		oi := &officialImages[i]
		if oi.OS == os && oi.Architecture == arch {
			if version == "" || oi.OSVersion == version {
				return oi, nil
			}
		}
	}

	return nil, fmt.Errorf("official image not found for os=%s version=%s arch=%s", os, version, arch)
}


func ResolveImageAlias(image string) (*domain.OfficialImage, error) {
	image = strings.ToLower(strings.TrimSpace(image))
	if image == "" {
		return nil, fmt.Errorf("image alias is empty")
	}


	for i := range officialImages {
		if officialImages[i].Name == image {
			return &officialImages[i], nil
		}
	}


	if alias, ok := aliases[image]; ok {
		for i := range officialImages {
			if officialImages[i].Name == alias {
				return &officialImages[i], nil
			}
		}
	}


	parts := strings.Split(image, "-")
	if len(parts) >= 1 {
		os := parts[0]
		version := ""
		arch := "amd64"
		if len(parts) == 2 {

			if parts[1] == "amd64" || parts[1] == "arm64" {
				arch = parts[1]
			} else {
				version = parts[1]
			}
		} else if len(parts) >= 3 {
			version = parts[1]
			arch = parts[2]
		}
		if img, err := Find(os, version, arch); err == nil {
			return img, nil
		}
	}

	return nil, fmt.Errorf("unknown image alias %q, try ubuntu, ubuntu-22.04, ubuntu-24.04, debian, debian-12", image)
}
