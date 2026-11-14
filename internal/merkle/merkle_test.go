package merkle

import (
	"testing"
)

func TestLeafBindsPathLengthAndContent(t *testing.T) {
	base, _ := HashLeaf("results/a.csv", []byte("abc"))
	changedPath, _ := HashLeaf("results/b.csv", []byte("abc"))
	changedLength, _ := HashLeaf("results/a.csv", []byte("abcd"))
	changedContent, _ := HashLeaf("results/a.csv", []byte("abd"))
	if base == changedPath || base == changedLength || base == changedContent {
		t.Fatal("leaf did not bind path, length, and content")
	}
	precomposed, _ := HashLeaf("caf\u00e9.txt", []byte("x"))
	decomposed, _ := HashLeaf("cafe\u0301.txt", []byte("x"))
	if precomposed != decomposed {
		t.Fatal("paths were not NFC-normalized")
	}
}

func TestOddAndEvenTreesVerifyEveryProof(t *testing.T) {
	for _, files := range []map[string][]byte{
		{"a": []byte("1"), "b": []byte("2"), "c": []byte("3")},
		{"a": []byte("1"), "b": []byte("2"), "c": []byte("3"), "d": []byte("4")},
	} {
		tree, err := New(files)
		if err != nil {
			t.Fatal(err)
		}
		for index, leaf := range tree.Leaves {
			proof, err := tree.Proof(index)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyProof(leaf.Content, proof, tree.Root()); err != nil {
				t.Fatalf("proof %d: %v", index, err)
			}
		}
	}
}

func TestWrongDirectionAndMutationFail(t *testing.T) {
	tree, err := New(map[string][]byte{"a": []byte("1"), "b": []byte("2"), "c": []byte("3")})
	if err != nil {
		t.Fatal(err)
	}
	proof, _ := tree.Proof(1)
	proof.Siblings[0].SiblingOnLeft = !proof.Siblings[0].SiblingOnLeft
	if err := VerifyProof([]byte("2"), proof, tree.Root()); err == nil {
		t.Fatal("wrong direction verified")
	}
	proof, _ = tree.Proof(1)
	if err := VerifyProof([]byte("9"), proof, tree.Root()); err == nil {
		t.Fatal("mutated content verified")
	}
}

func TestRejectsEmptyTreeAndNonCanonicalPath(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("empty tree accepted")
	}
	if _, err := HashLeaf("../escape", nil); err == nil {
		t.Fatal("parent path accepted")
	}
	if _, err := HashLeaf("a\\b", nil); err == nil {
		t.Fatal("backslash path accepted")
	}
	if _, err := ParseHash("not-a-hash"); err == nil {
		t.Fatal("invalid hash accepted")
	}
}

func TestProofRejectsInvalidIndexSiblingAndRoot(t *testing.T) {
	tree, _ := New(map[string][]byte{"a": []byte("1"), "b": []byte("2")})
	if _, err := tree.Proof(-1); err == nil {
		t.Fatal("negative proof index accepted")
	}
	proof, _ := tree.Proof(0)
	proof.Index = 8
	if err := VerifyProof([]byte("1"), proof, tree.Root()); err == nil {
		t.Fatal("out-of-depth index verified")
	}
	proof, _ = tree.Proof(0)
	proof.Siblings[0].Hash = "00"
	if err := VerifyProof([]byte("1"), proof, tree.Root()); err == nil {
		t.Fatal("short sibling verified")
	}
	proof, _ = tree.Proof(0)
	wrongRoot, _ := ParseHash("0000000000000000000000000000000000000000000000000000000000000000")
	if err := VerifyProof([]byte("1"), proof, wrongRoot); err == nil {
		t.Fatal("wrong root verified")
	}
}
