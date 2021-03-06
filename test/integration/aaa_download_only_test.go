// +build integration

/*
Copyright 2019 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration

import (
	"context"
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"k8s.io/minikube/pkg/minikube/bootstrapper/images"
	"k8s.io/minikube/pkg/minikube/constants"
	"k8s.io/minikube/pkg/minikube/download"
	"k8s.io/minikube/pkg/minikube/localpath"
)

func TestDownloadOnly(t *testing.T) {
	for _, r := range []string{"crio", "docker", "containerd"} {
		t.Run(r, func(t *testing.T) {
			// Stores the startup run result for later error messages
			var rrr *RunResult
			var err error

			profile := UniqueProfileName(r)
			ctx, cancel := context.WithTimeout(context.Background(), Minutes(30))
			defer Cleanup(t, profile, cancel)

			versions := []string{
				constants.OldestKubernetesVersion,
				constants.DefaultKubernetesVersion,
				constants.NewestKubernetesVersion,
			}

			for _, v := range versions {
				t.Run(v, func(t *testing.T) {
					// Explicitly does not pass StartArgs() to test driver default
					// --force to avoid uid check
					args := append([]string{"start", "--download-only", "-p", profile, "--force", "--alsologtostderr", fmt.Sprintf("--kubernetes-version=%s", v), fmt.Sprintf("--container-runtime=%s", r)}, StartArgs()...)

					// Preserve the initial run-result for debugging
					if rrr == nil {
						rrr, err = Run(t, exec.CommandContext(ctx, Target(), args...))
					} else {
						_, err = Run(t, exec.CommandContext(ctx, Target(), args...))
					}

					if err != nil {
						t.Errorf("%s failed: %v", args, err)
					}

					if download.PreloadExists(v, r) {
						// Just make sure the tarball path exists
						if _, err := os.Stat(download.TarballPath(v)); err != nil {
							t.Errorf("preloaded tarball path doesn't exist: %v", err)
						}
						return
					}

					imgs, err := images.Kubeadm("", v)
					if err != nil {
						t.Errorf("kubeadm images: %v %+v", v, err)
					}

					// skip verify for cache images if --driver=none
					if !NoneDriver() {
						for _, img := range imgs {
							img = strings.Replace(img, ":", "_", 1) // for example kube-scheduler:v1.15.2 --> kube-scheduler_v1.15.2
							fp := filepath.Join(localpath.MiniPath(), "cache", "images", img)
							_, err := os.Stat(fp)
							if err != nil {
								t.Errorf("expected image file exist at %q but got error: %v", fp, err)
							}
						}
					}

					// checking binaries downloaded (kubelet,kubeadm)
					for _, bin := range constants.KubernetesReleaseBinaries {
						fp := filepath.Join(localpath.MiniPath(), "cache", "linux", v, bin)
						_, err := os.Stat(fp)
						if err != nil {
							t.Errorf("expected the file for binary exist at %q but got error %v", fp, err)
						}
					}

					// If we are on darwin/windows, check to make sure OS specific kubectl has been downloaded
					// as well for the `minikube kubectl` command
					if runtime.GOOS == "linux" {
						return
					}
					binary := "kubectl"
					if runtime.GOOS == "windows" {
						binary = "kubectl.exe"
					}
					fp := filepath.Join(localpath.MiniPath(), "cache", runtime.GOOS, v, binary)
					if _, err := os.Stat(fp); err != nil {
						t.Errorf("expected the file for binary exist at %q but got error %v", fp, err)
					}
				})
			}

			// This is a weird place to test profile deletion, but this test is serial, and we have a profile to delete!
			t.Run("DeleteAll", func(t *testing.T) {
				if !CanCleanup() {
					t.Skip("skipping, as cleanup is disabled")
				}
				rr, err := Run(t, exec.CommandContext(ctx, Target(), "delete", "--all"))
				if err != nil {
					t.Errorf("%s failed: %v", rr.Args, err)
				}
			})
			// Delete should always succeed, even if previously partially or fully deleted.
			t.Run("DeleteAlwaysSucceeds", func(t *testing.T) {
				if !CanCleanup() {
					t.Skip("skipping, as cleanup is disabled")
				}
				rr, err := Run(t, exec.CommandContext(ctx, Target(), "delete", "-p", profile))
				if err != nil {
					t.Errorf("%s failed: %v", rr.Args, err)
				}
			})
		})
	}
}

func TestDownloadOnlyDocker(t *testing.T) {
	if !runningDockerDriver(StartArgs()) {
		t.Skip("this test only runs with the docker driver")
	}

	profile := UniqueProfileName("download-docker")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer Cleanup(t, profile, cancel)

	args := []string{"start", "--download-only", "-p", profile, "--force", "--alsologtostderr", "--driver=docker"}
	rr, err := Run(t, exec.CommandContext(ctx, Target(), args...))
	if err != nil {
		t.Errorf("%s failed: %v:\n%s", args, err, rr.Output())
	}

	// Make sure the downloaded image tarball exists
	tarball := download.TarballPath(constants.DefaultKubernetesVersion)
	contents, err := ioutil.ReadFile(tarball)
	if err != nil {
		t.Errorf("reading tarball: %v", err)
	}
	// Make sure it has the correct checksum
	checksum := md5.Sum(contents)
	remoteChecksum, err := ioutil.ReadFile(download.PreloadChecksumPath(constants.DefaultKubernetesVersion))
	if err != nil {
		t.Errorf("reading checksum file: %v", err)
	}
	if string(remoteChecksum) != string(checksum[:]) {
		t.Errorf("checksum of %s does not match remote checksum (%s != %s)", tarball, string(remoteChecksum), string(checksum[:]))
	}
}

func runningDockerDriver(startArgs []string) bool {
	for _, s := range startArgs {
		if s == "--driver=docker" {
			return true
		}
	}
	return false
}
