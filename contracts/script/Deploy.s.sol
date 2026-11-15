// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { BacktestRegistry } from "../src/BacktestRegistry.sol";

interface Vm {
    function envUint(string calldata) external returns (uint256);
    function startBroadcast(uint256) external;
    function stopBroadcast() external;
}

contract Deploy {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    function run() external returns (BacktestRegistry registry) {
        uint256 privateKey = vm.envUint("PRIVATE_KEY");
        vm.startBroadcast(privateKey);
        registry = new BacktestRegistry();
        vm.stopBroadcast();
    }
}
