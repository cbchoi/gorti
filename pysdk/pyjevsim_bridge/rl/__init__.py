"""Reinforcement-learning interfaces for pyjevsim and gorti.

The core is dependency-free and Gymnasium-shaped.  Model authors provide an
``EpisodeFactory`` that builds a fresh pyjevsim ``SysExecutor`` graph on every
reset; the same environment can then be hosted by ``LocalRolloutPool`` or a
``GortiRolloutChannel``.
"""

from pyjevsim_bridge.rl.adapters import FunctionalEpisodeBinding
from pyjevsim_bridge.rl.contracts import (
    EnvironmentClosedError,
    EpisodeBinding,
    EpisodeContext,
    EpisodeFactory,
    EpisodeStateError,
    EpisodeStepError,
    ExecutorContractError,
    ExecutorProtocol,
    RLEnvironmentError,
    StepView,
)
from pyjevsim_bridge.rl.environment import PyJevSimEnv
from pyjevsim_bridge.rl.executor import (
    BindingDecisionBoundary,
    DecisionBoundary,
    ExecutorDriver,
    ExecutorStep,
    FixedDeltaBoundary,
    NextEventBoundary,
)
from pyjevsim_bridge.rl.federation import (
    ACTION_CLASS,
    CONTROL_CLASS,
    POLICY_CLASS,
    TRANSITION_CLASS,
    EnvelopeValidationError,
    EventStreamExhaustedError,
    FederationHaltedError,
    FederationProtocolError,
    GortiRolloutChannel,
    GrantedBatch,
    IdempotencyConflictError,
    ReceivedEnvelope,
    Role,
    canonical_json,
    decode_envelope,
    encode_envelope,
    validate_envelope,
)
from pyjevsim_bridge.rl.local import (
    LocalRolloutBatchError,
    LocalRolloutPool,
    derive_episode_seed,
)
from pyjevsim_bridge.rl.records import ActionCommand, ResetResult, TransitionRecord

__all__ = [
    "ACTION_CLASS",
    "CONTROL_CLASS",
    "POLICY_CLASS",
    "TRANSITION_CLASS",
    "ActionCommand",
    "BindingDecisionBoundary",
    "DecisionBoundary",
    "EnvelopeValidationError",
    "EnvironmentClosedError",
    "EpisodeBinding",
    "EpisodeContext",
    "EpisodeFactory",
    "EpisodeStateError",
    "EpisodeStepError",
    "EventStreamExhaustedError",
    "ExecutorContractError",
    "ExecutorDriver",
    "ExecutorProtocol",
    "ExecutorStep",
    "FederationHaltedError",
    "FederationProtocolError",
    "FixedDeltaBoundary",
    "FunctionalEpisodeBinding",
    "GortiRolloutChannel",
    "GrantedBatch",
    "IdempotencyConflictError",
    "LocalRolloutPool",
    "LocalRolloutBatchError",
    "NextEventBoundary",
    "PyJevSimEnv",
    "RLEnvironmentError",
    "ReceivedEnvelope",
    "ResetResult",
    "Role",
    "StepView",
    "TransitionRecord",
    "canonical_json",
    "decode_envelope",
    "derive_episode_seed",
    "encode_envelope",
    "validate_envelope",
]
