// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @notice Stores one record per canonical evidence root and verifies ordered
/// SHA-256 inclusion proofs. File path/content binding remains an offline leaf concern.
contract BacktestRegistry {
    struct Record {
        address submitter;
        uint64 timestamp;
        bytes32 metadataHash;
    }

    error DuplicateRoot(bytes32 root);
    error LengthMismatch(uint256 siblings, uint256 directions);
    error UnknownRoot(bytes32 root);
    error ZeroRoot();

    event RootSubmitted(bytes32 indexed root, address indexed submitter, uint64 timestamp, bytes32 metadataHash);

    mapping(bytes32 => Record) public records;

    function submitRoot(bytes32 root, bytes32 metadataHash) external {
        if (root == bytes32(0)) revert ZeroRoot();
        if (records[root].submitter != address(0)) revert DuplicateRoot(root);

        uint64 timestamp = uint64(block.timestamp);
        records[root] = Record({ submitter: msg.sender, timestamp: timestamp, metadataHash: metadataHash });
        emit RootSubmitted(root, msg.sender, timestamp, metadataHash);
    }

    function verifyStoredProof(bytes32 root, bytes32 leaf, bytes32[] calldata siblings, bool[] calldata siblingOnLeft)
        external
        view
        returns (bool)
    {
        if (siblings.length != siblingOnLeft.length) {
            revert LengthMismatch(siblings.length, siblingOnLeft.length);
        }
        if (records[root].submitter == address(0)) revert UnknownRoot(root);

        bytes32 current = leaf;
        for (uint256 index = 0; index < siblings.length; ++index) {
            current = siblingOnLeft[index]
                ? sha256(abi.encodePacked(bytes1(0x01), siblings[index], current))
                : sha256(abi.encodePacked(bytes1(0x01), current, siblings[index]));
        }
        return current == root;
    }
}
