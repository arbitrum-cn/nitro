// SPDX-License-Identifier: Apache-2.0

/*
 * Copyright 2021, Offchain Labs, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

pragma solidity ^0.8.0;

struct Node {
    // Hash of the state of the chain as of this node
    bytes32 stateHash;
    // Hash of the data that can be challenged
    bytes32 challengeHash;
    // Hash of the data that will be committed if this node is confirmed
    bytes32 confirmData;
    // Index of the node previous to this one
    uint256 prevNum;
    // Deadline at which this node can be confirmed
    uint256 deadlineBlock;
    // Deadline at which a child of this node can be confirmed
    uint256 noChildConfirmedBeforeBlock;
    // Number of stakers staked on this node. This includes real stakers and zombies
    uint256 stakerCount;
    // This value starts at zero and is set to a value when the first child is created. After that it is constant until the node is destroyed or the owner destroys pending nodes
    uint256 firstChildBlock;
    // The number of the latest child of this node to be created
    uint256 latestChildNumber;
    // The block number when this node was created
    uint256 createdAtBlock;
}

/**
 * @notice Utility functions for Node
 */
library NodeLib {
    /**
     * @notice Initialize a Node
     * @param _stateHash Initial value of stateHash
     * @param _challengeHash Initial value of challengeHash
     * @param _confirmData Initial value of confirmData
     * @param _prevNum Initial value of prevNum
     * @param _deadlineBlock Initial value of deadlineBlock
     */
    function initialize(
        bytes32 _stateHash,
        bytes32 _challengeHash,
        bytes32 _confirmData,
        uint256 _prevNum,
        uint256 _deadlineBlock
    ) internal view returns (Node memory) {
        Node memory node;
        node.stateHash = _stateHash;
        node.challengeHash = _challengeHash;
        node.confirmData = _confirmData;
        node.prevNum = _prevNum;
        node.deadlineBlock = _deadlineBlock;
        node.noChildConfirmedBeforeBlock = _deadlineBlock;
        node.createdAtBlock = block.number;
        return node;
    }

    /**
     * @notice Update child properties
     * @param number The child number to set
     */
    function childCreated(Node storage self, uint256 number) internal {
        if (self.firstChildBlock == 0) {
            self.firstChildBlock = block.number;
        }
        self.latestChildNumber = number;
    }

    /**
     * @notice Update the child confirmed deadline
     * @param deadline The new deadline to set
     */
    function newChildConfirmDeadline(Node storage self, uint256 deadline) internal {
        self.noChildConfirmedBeforeBlock = deadline;
    }

    /**
     * @notice Check whether the current block number has met or passed the node's deadline
     */
    function requirePastDeadline(Node memory self) internal view {
        require(block.number >= self.deadlineBlock, "BEFORE_DEADLINE");
    }

    /**
     * @notice Check whether the current block number has met or passed deadline for children of this node to be confirmed
     */
    function requirePastChildConfirmDeadline(Node memory self) internal view {
        require(block.number >= self.noChildConfirmedBeforeBlock, "CHILD_TOO_RECENT");
    }
}