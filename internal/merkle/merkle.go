package merkle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

type Leaf struct {
	Path        string
	Content     []byte
	ContentHash [32]byte
	Hash        [32]byte
}

type ProofStep struct {
	Hash          string `json:"hash"`
	SiblingOnLeft bool   `json:"sibling_on_left"`
}

type Proof struct {
	ContentLength uint64      `json:"content_length"`
	ContentSHA256 string      `json:"content_sha256"`
	Index         int         `json:"index"`
	LeafHash      string      `json:"leaf_hash"`
	Path          string      `json:"path"`
	SchemaVersion string      `json:"schema_version"`
	Siblings      []ProofStep `json:"siblings"`
}

type Tree struct {
	Leaves []Leaf
	levels [][][32]byte
}

func New(files map[string][]byte) (*Tree, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("empty Merkle tree is invalid")
	}
	byPath := make(map[string][]byte, len(files))
	paths := make([]string, 0, len(files))
	for rawPath, content := range files {
		normalized, err := NormalizePath(rawPath)
		if err != nil {
			return nil, err
		}
		if _, exists := byPath[normalized]; exists {
			return nil, fmt.Errorf("paths collide after NFC normalization: %s", normalized)
		}
		byPath[normalized] = append([]byte(nil), content...)
		paths = append(paths, normalized)
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })
	tree := &Tree{Leaves: make([]Leaf, 0, len(paths))}
	first := make([][32]byte, 0, len(paths))
	for _, name := range paths {
		content := byPath[name]
		leafHash, err := HashLeaf(name, content)
		if err != nil {
			return nil, err
		}
		leaf := Leaf{Path: name, Content: content, ContentHash: sha256.Sum256(content), Hash: leafHash}
		tree.Leaves = append(tree.Leaves, leaf)
		first = append(first, leafHash)
	}
	tree.levels = append(tree.levels, first)
	for len(tree.levels[len(tree.levels)-1]) > 1 {
		current := tree.levels[len(tree.levels)-1]
		next := make([][32]byte, 0, (len(current)+1)/2)
		for index := 0; index < len(current); index += 2 {
			right := current[index]
			if index+1 < len(current) {
				right = current[index+1]
			}
			next = append(next, HashNode(current[index], right))
		}
		tree.levels = append(tree.levels, next)
	}
	return tree, nil
}

func NormalizePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("invalid bundle path %q", value)
	}
	normalized := norm.NFC.String(value)
	if path.Clean(normalized) != normalized || normalized == "." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("bundle path is not canonical: %q", value)
	}
	return normalized, nil
}

func HashLeaf(name string, content []byte) ([32]byte, error) {
	normalized, err := NormalizePath(name)
	if err != nil {
		return [32]byte{}, err
	}
	pathBytes := []byte(normalized)
	if uint64(len(pathBytes)) > uint64(^uint32(0)) {
		return [32]byte{}, fmt.Errorf("path is too long")
	}
	input := make([]byte, 1+4+len(pathBytes)+8+len(content))
	input[0] = 0x00
	binary.BigEndian.PutUint32(input[1:5], uint32(len(pathBytes)))
	copy(input[5:], pathBytes)
	binary.BigEndian.PutUint64(input[5+len(pathBytes):13+len(pathBytes)], uint64(len(content)))
	copy(input[13+len(pathBytes):], content)
	return sha256.Sum256(input), nil
}

func HashNode(left, right [32]byte) [32]byte {
	input := make([]byte, 65)
	input[0] = 0x01
	copy(input[1:33], left[:])
	copy(input[33:], right[:])
	return sha256.Sum256(input)
}

func (tree *Tree) Root() [32]byte { return tree.levels[len(tree.levels)-1][0] }

func (tree *Tree) RootHex() string {
	root := tree.Root()
	return hex.EncodeToString(root[:])
}

func (tree *Tree) Proof(index int) (Proof, error) {
	if index < 0 || index >= len(tree.Leaves) {
		return Proof{}, fmt.Errorf("leaf index out of range")
	}
	leaf := tree.Leaves[index]
	proof := Proof{
		ContentLength: uint64(len(leaf.Content)), ContentSHA256: hex.EncodeToString(leaf.ContentHash[:]),
		Index: index, LeafHash: hex.EncodeToString(leaf.Hash[:]), Path: leaf.Path,
		SchemaVersion: "1.0.0", Siblings: make([]ProofStep, 0, len(tree.levels)-1),
	}
	position := index
	for level := 0; level < len(tree.levels)-1; level++ {
		nodes := tree.levels[level]
		siblingIndex := position + 1
		siblingOnLeft := position%2 == 1
		if siblingOnLeft {
			siblingIndex = position - 1
		} else if siblingIndex >= len(nodes) {
			siblingIndex = position
		}
		proof.Siblings = append(proof.Siblings, ProofStep{Hash: hex.EncodeToString(nodes[siblingIndex][:]), SiblingOnLeft: siblingOnLeft})
		position /= 2
	}
	return proof, nil
}

func VerifyProof(content []byte, proof Proof, root [32]byte) error {
	if proof.SchemaVersion != "1.0.0" || proof.Index < 0 || proof.ContentLength != uint64(len(content)) {
		return fmt.Errorf("invalid proof metadata")
	}
	contentHash := sha256.Sum256(content)
	if hex.EncodeToString(contentHash[:]) != proof.ContentSHA256 {
		return fmt.Errorf("content hash mismatch")
	}
	current, err := HashLeaf(proof.Path, content)
	if err != nil {
		return err
	}
	if hex.EncodeToString(current[:]) != proof.LeafHash {
		return fmt.Errorf("leaf hash mismatch")
	}
	position := proof.Index
	for _, step := range proof.Siblings {
		siblingBytes, err := hex.DecodeString(step.Hash)
		if err != nil || len(siblingBytes) != 32 {
			return fmt.Errorf("invalid sibling hash")
		}
		var sibling [32]byte
		copy(sibling[:], siblingBytes)
		wantLeft := position%2 == 1
		if step.SiblingOnLeft != wantLeft {
			return fmt.Errorf("proof direction does not match leaf index")
		}
		if step.SiblingOnLeft {
			current = HashNode(sibling, current)
		} else {
			current = HashNode(current, sibling)
		}
		position /= 2
	}
	if position != 0 {
		return fmt.Errorf("proof index exceeds tree depth")
	}
	if current != root {
		return fmt.Errorf("proof root mismatch")
	}
	return nil
}

func ParseHash(value string) ([32]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, fmt.Errorf("invalid SHA-256 hash")
	}
	var result [32]byte
	copy(result[:], decoded)
	return result, nil
}
