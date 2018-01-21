package main

import (
	"testing"
)

func TestRepoPackage(t *testing.T) {
	r := &repo{
		DownloadRoot: "private/test/skel",
	}

	if err := r.checkCompressedFile("archive.ubuntu.com/ubuntu/dists/xenial/main/binary-amd64/Packages", distMetafile{
		sha256: "f6bd998e33ce5cc6fa43df47f191a78587e658b1b7fd29c7d065742acd4e5dd9",
	}); err != nil {
		t.Fatal(err)
	}
}
