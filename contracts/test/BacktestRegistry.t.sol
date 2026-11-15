// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { BacktestRegistry } from "../src/BacktestRegistry.sol";

interface Vm {
    function expectEmit(bool, bool, bool, bool, address) external;
    function expectRevert(bytes4) external;
    function expectRevert(bytes calldata) external;
    function warp(uint256) external;
}

contract BacktestRegistryTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    BacktestRegistry private registry;

    event RootSubmitted(bytes32 indexed root, address indexed submitter, uint64 timestamp, bytes32 metadataHash);

    function setUp() public {
        registry = new BacktestRegistry();
    }

    function testSubmitStoresRecordAndEmitsEvent() public {
        bytes32 root = sha256("root");
        bytes32 metadataHash = sha256("metadata");
        vm.warp(1_735_689_600);
        vm.expectEmit(true, true, false, true, address(registry));
        emit RootSubmitted(root, address(this), 1_735_689_600, metadataHash);

        registry.submitRoot(root, metadataHash);
        (address submitter, uint64 timestamp, bytes32 storedMetadata) = registry.records(root);
        require(submitter == address(this), "submitter mismatch");
        require(timestamp == 1_735_689_600, "timestamp mismatch");
        require(storedMetadata == metadataHash, "metadata mismatch");
    }

    function testRejectsZeroAndDuplicateRoots() public {
        vm.expectRevert(BacktestRegistry.ZeroRoot.selector);
        registry.submitRoot(bytes32(0), bytes32(0));

        bytes32 root = sha256("root");
        registry.submitRoot(root, bytes32(0));
        vm.expectRevert(abi.encodeWithSelector(BacktestRegistry.DuplicateRoot.selector, root));
        registry.submitRoot(root, bytes32(0));
    }

    function testVerifiesOrderedProofAndRejectsMutations() public {
        (bytes32 root, bytes32 leaf, bytes32[] memory siblings, bool[] memory directions) = fixture();
        registry.submitRoot(root, bytes32(0));
        require(registry.verifyStoredProof(root, leaf, siblings, directions), "valid proof rejected");

        require(
            !registry.verifyStoredProof(root, bytes32(uint256(leaf) ^ 1), siblings, directions),
            "leaf mutation accepted"
        );
        siblings[0] = bytes32(uint256(siblings[0]) ^ 1);
        require(!registry.verifyStoredProof(root, leaf, siblings, directions), "sibling mutation accepted");
        siblings[0] = sha256("leaf-0");
        directions[0] = false;
        require(!registry.verifyStoredProof(root, leaf, siblings, directions), "direction mutation accepted");
    }

    function testRejectsUnknownRootAndLengthMismatch() public {
        bytes32[] memory siblings = new bytes32[](1);
        bool[] memory directions = new bool[](0);
        bytes32 unknownRoot = sha256("unknown");
        bytes32 leaf = sha256("leaf");
        vm.expectRevert(abi.encodeWithSelector(BacktestRegistry.UnknownRoot.selector, unknownRoot));
        registry.verifyStoredProof(unknownRoot, leaf, siblings, siblingsToDirections(siblings));

        bytes32 root = sha256("root");
        registry.submitRoot(root, bytes32(0));
        vm.expectRevert(abi.encodeWithSelector(BacktestRegistry.LengthMismatch.selector, 1, 0));
        registry.verifyStoredProof(root, leaf, siblings, directions);
    }

    function testFuzzSingleLevelOrderedProof(bytes32 leaf, bytes32 sibling, bool siblingOnLeft) public {
        bytes32 root = siblingOnLeft ? hashNode(sibling, leaf) : hashNode(leaf, sibling);
        if (root == bytes32(0)) return;
        registry.submitRoot(root, bytes32(0));
        bytes32[] memory siblings = new bytes32[](1);
        siblings[0] = sibling;
        bool[] memory directions = new bool[](1);
        directions[0] = siblingOnLeft;
        require(registry.verifyStoredProof(root, leaf, siblings, directions), "fuzz proof rejected");
    }

    function fixture()
        private
        pure
        returns (bytes32 root, bytes32 leaf, bytes32[] memory siblings, bool[] memory directions)
    {
        bytes32 leaf0 = sha256("leaf-0");
        leaf = sha256("leaf-1");
        bytes32 leaf2 = sha256("leaf-2");
        root = hashNode(hashNode(leaf0, leaf), hashNode(leaf2, leaf2));
        siblings = new bytes32[](2);
        siblings[0] = leaf0;
        siblings[1] = hashNode(leaf2, leaf2);
        directions = new bool[](2);
        directions[0] = true;
        directions[1] = false;
    }

    function hashNode(bytes32 left, bytes32 right) private pure returns (bytes32) {
        return sha256(abi.encodePacked(bytes1(0x01), left, right));
    }

    function siblingsToDirections(bytes32[] memory siblings) private pure returns (bool[] memory directions) {
        directions = new bool[](siblings.length);
    }
}
