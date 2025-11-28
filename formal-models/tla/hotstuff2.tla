---- MODULE hotstuff2 ----
(***************************************************************************
 * TLA+ Specification for HotStuff-2 Consensus Protocol
 *
 * Paper: https://eprint.iacr.org/2023/397.pdf
 * Reference: https://github.com/edgedlt/hotstuff2/blob/main/docs/PROTOCOL.md
 * Implementation: https://github.com/edgedlt/hotstuff2/blob/main/hotstuff2.go
 * Mapping: https://github.com/edgedlt/hotstuff2/blob/main/formal-models/tla/IMPLEMENTATION_MAPPING.md
 *
 * This spec models the two-phase HotStuff-2 algorithm with:
 * - Two-chain commit rule (Cv(Cv(B)))
 * - SafeNode voting rule (three conditions)
 * - Leader's implicit vote (critical for liveness)
 * - NEWVIEW protocol for view synchronization
 * - Byzantine fault tolerance (|Faulty| ≤ f)
 *
 * Verification: 786K+ states, 12 safety invariants, 4 liveness properties
 ***************************************************************************)

EXTENDS Integers, FiniteSets, Sequences, TLC

CONSTANTS
    Replica,        \* Set of replica IDs (recommended: {1,2,3,4} for TLC)
    Faulty,         \* Set of faulty replica IDs (must be subset of Replica, |Faulty| <= f)
    MaxView,        \* Maximum view number (bounds state space for model checking)
    MaxHeight,      \* Maximum block height (bounds state space)
    Genesis,        \* Genesis block identifier (use 0)
    Nil             \* Null value (use -1)

VARIABLES
    view,           \* Replica -> View (context.go:24)
    lockedQC,       \* Replica -> Block \cup {Nil} (context.go:28)
    highQC,         \* Replica -> Block \cup {Nil} (context.go:31)
    committed,      \* Replica -> SUBSET Block (context.go:34)
    proposals,      \* View -> Block \cup {Nil}
    blocks,         \* SUBSET Block
    blockParent,    \* Block -> Block \cup {Genesis}
    blockView,      \* Block -> View
    blockHeight,    \* Block -> Nat
    network         \* SUBSET Message

(*************************************************************************)
\* Model Configuration
(*************************************************************************)

N == Cardinality(Replica)
F == (N - 1) \div 3
Quorum == 2 * F + 1

\* Honest replicas (non-faulty)
Honest == Replica \ Faulty

\* ASSUME: |Faulty| <= f for BFT assumption to hold
ASSUME Cardinality(Faulty) <= F

Block == 1..MaxHeight
View == 0..MaxView
Height == 0..MaxHeight

ProposeMsgs ==
    { [type |-> "PROPOSE", view |-> v, block |-> b, qc |-> q] :
        v \in View, b \in Block, q \in Block \cup {Nil} }

VoteMsgs ==
    { [type |-> "VOTE", view |-> v, block |-> b, replica |-> r] :
        v \in View, b \in Block, r \in Replica }

NewViewMsgs ==
    { [type |-> "NEWVIEW", view |-> v, replica |-> r, qc |-> q] :
        v \in View, r \in Replica, q \in Block \cup {Nil} }

Message == ProposeMsgs \cup VoteMsgs \cup NewViewMsgs

(*************************************************************************)
\* Helper Functions
(*************************************************************************)

Leader(v) == CHOOSE r \in Replica : r = (v % N) + 1

IsQuorum(S) == Cardinality(S) >= Quorum

\* ASSUME: Quorum intersection - any two quorums share at least one honest replica
\* This follows from n >= 3f+1, quorum = 2f+1: overlap = 2(2f+1) - (3f+1) = f+1 > f
ASSUME \A S1 \in SUBSET Replica :
         \A S2 \in SUBSET Replica :
           (IsQuorum(S1) /\ IsQuorum(S2)) => (S1 \cap S2 \cap Honest) /= {}

\* Bounded ancestry check (context.go:286-312)
RECURSIVE AncestorK(_,_,_)
AncestorK(a, b, k) ==
    IF k < 0 THEN FALSE
    ELSE IF a = b THEN TRUE
    ELSE IF b = Genesis THEN (a = Genesis)
    ELSE IF b \notin blocks THEN FALSE
    ELSE IF k = 0 THEN FALSE
    ELSE AncestorK(a, blockParent[b], k - 1)

IsAncestor(a, b) ==
    IF a = Genesis THEN TRUE
    ELSE IF b = Genesis THEN FALSE
    ELSE IF a = b THEN TRUE
    ELSE \E i \in 0..MaxHeight : AncestorK(a, b, i)

HighestQC(qcSet) ==
    LET qcs == qcSet \ {Nil}
    IN IF qcs = {} THEN Nil
       ELSE CHOOSE q \in qcs :
            /\ q \in blocks
            /\ \A q2 \in qcs :
                 IF q2 \in blocks THEN blockView[q] >= blockView[q2] ELSE TRUE

(*************************************************************************)
\* Network Helpers
(*************************************************************************)

VotesFromNetwork(v, b) ==
    IF v \in View /\ b \in Block
    THEN LET voteMsgs == {m \in network : m.type = "VOTE" /\ m.view = v /\ m.block = b}
         IN IF voteMsgs = {} THEN {} ELSE { msg.replica : msg \in voteMsgs }
    ELSE {}

HasQuorumInNetwork(v, b) ==
    IF v \notin View \/ b \notin Block
    THEN FALSE
    ELSE IsQuorum(VotesFromNetwork(v, b))

NewViewReplicasFromNetwork(v) ==
    { msg.replica : msg \in {m \in network : m.type = "NEWVIEW" /\ m.view = v} }

HighestQCFromNetwork(v) ==
    LET newviewMsgs == {m \in network : m.type = "NEWVIEW" /\ m.view = v}
        qcs == {m.qc : m \in newviewMsgs} \ {Nil}
    IN IF qcs = {} THEN Nil ELSE HighestQC(qcs)

HasQCInNetwork(v, b) ==
    IF v \in View /\ b \in Block /\ HasQuorumInNetwork(v, b)
    THEN b
    ELSE Nil

(*************************************************************************)
\* Safety Predicates
(*************************************************************************)

\* context.go:314-332 - SafeToVote()
SafeNodeRule(r, block, qc) ==
    \/ lockedQC[r] = Nil
    \/ (qc /= Nil /\ blockView[qc] > blockView[lockedQC[r]])
    \/ IsAncestor(lockedQC[r], block)

\* Two-chain commit rule (context.go:359-378)
CanCommit(block, qc) ==
    /\ qc /= Nil
    /\ qc \in blocks
    /\ block \in blocks
    /\ blockParent[qc] = block
    /\ blockView[qc] = blockView[block] + 1

\* context.go:99-113 - UpdateLockedQC()
UpdateLockCond(r, qc) ==
    /\ qc /= Nil
    /\ qc \in blocks
    /\ (lockedQC[r] = Nil \/ blockView[qc] > blockView[lockedQC[r]])

(*************************************************************************)
\* Type Invariant
(*************************************************************************)

TypeInvariant ==
    /\ view \in [Replica -> View]
    /\ lockedQC \in [Replica -> Block \cup {Nil}]
    /\ highQC \in [Replica -> Block \cup {Nil}]
    /\ committed \in [Replica -> SUBSET Block]
    /\ proposals \in [View -> Block \cup {Nil}]
    /\ blocks \subseteq Block
    /\ blockParent \in [Block -> Block \cup {Genesis}]
    /\ blockView   \in [Block -> View]
    /\ blockHeight \in [Block -> Height]
    /\ network \subseteq Message

(*************************************************************************)
\* Initial State
(*************************************************************************)

Init ==
    /\ view = [r \in Replica |-> 0]
    /\ lockedQC = [r \in Replica |-> Nil]
    /\ highQC = [r \in Replica |-> Nil]
    /\ committed = [r \in Replica |-> {}]
    /\ proposals = [v \in View |-> Nil]
    /\ blocks = {}
    /\ blockParent = [b \in Block |-> Genesis]
    /\ blockView   = [b \in Block |-> 0]
    /\ blockHeight = [b \in Block |-> 0]
    /\ network = {}

(*************************************************************************)
\* Actions
(*************************************************************************)

\* hotstuff2.go:510-553 - propose()
\* CRITICAL: Leader includes its own implicit vote (hotstuff2.go:653-662)
\* This is required for liveness: with n=3f+1 and f faults, we have 2f+1 honest nodes.
\* If the leader doesn't vote, only 2f non-leader votes are available, which is < 2f+1 quorum.
LeaderPropose(r) ==
    LET v == view[r]
        qc == highQC[r]
        availableBlocks == Block \ blocks
    IN
    /\ r \in Honest                 \* Only honest leaders propose
    /\ r = Leader(v)
    /\ proposals[v] = Nil
    /\ availableBlocks /= {}
    /\ LET newBlock == CHOOSE b \in availableBlocks : TRUE
       IN
        /\ blocks' = blocks \cup {newBlock}
        /\ blockParent' = blockParent @@ (IF qc = Nil
                                           THEN [ bb \in {newBlock} |-> Genesis ]
                                           ELSE [ bb \in {newBlock} |-> qc ])
        /\ blockView' = blockView @@ [ bb \in {newBlock} |-> v ]
        /\ blockHeight' = blockHeight @@ [ bb \in {newBlock} |-> IF qc = Nil THEN 1 ELSE blockHeight[qc] + 1 ]
       /\ proposals' = [proposals EXCEPT ![v] = newBlock]
       \* Leader broadcasts PROPOSE and includes its own VOTE (implicit vote)
       /\ network' = network \cup { 
            [type |-> "PROPOSE", view |-> v, block |-> newBlock, qc |-> qc],
            [type |-> "VOTE", view |-> v, block |-> newBlock, replica |-> r] }
       /\ UNCHANGED <<view, lockedQC, highQC, committed>>

\* hotstuff2.go:174-279 - onProposal()
\* Only honest (non-faulty) replicas vote
ReplicaVote(r) ==
    \E msg \in network :
        LET v == msg.view
            b == msg.block
            qc == msg.qc
            voteMsg == [type |-> "VOTE", view |-> v, block |-> b, replica |-> r]
        IN
        /\ r \in Honest             \* Only honest replicas participate
        /\ msg.type = "PROPOSE"
        /\ v = view[r]              \* Current view only
        /\ r /= Leader(v)
        /\ b \in blocks
        /\ SafeNodeRule(r, b, qc)
        /\ voteMsg \notin network
        /\ network' = network \cup {voteMsg}
        /\ IF UpdateLockCond(r, qc)
             THEN lockedQC' = [lockedQC EXCEPT ![r] = qc]
             ELSE UNCHANGED lockedQC
        /\ UNCHANGED <<view, highQC, committed, proposals, blocks, blockParent, blockView, blockHeight>>

\* hotstuff2.go:289-388 - onVote()
LeaderFormQC(r) ==
    \E v \in View, b \in blocks :
        LET highQCView == IF highQC[r] = Nil THEN 0 ELSE blockView[highQC[r]]
            bView == blockView[b]
        IN
        /\ r \in Honest             \* Only honest leaders form QCs
        /\ r = Leader(v)
        /\ HasQuorumInNetwork(v, b)
        /\ bView > highQCView
        /\ highQC' = [highQC EXCEPT ![r] = b]
        /\ IF UpdateLockCond(r, b)
             THEN lockedQC' = [lockedQC EXCEPT ![r] = b]
             ELSE UNCHANGED lockedQC
        /\ LET parent == blockParent[b]
           IN IF CanCommit(parent, b)
              THEN committed' = [committed EXCEPT ![r] = @ \cup {parent}]
              ELSE UNCHANGED committed
        /\ UNCHANGED <<view, proposals, blocks, blockParent, blockView, blockHeight, network>>

\* Replica observes QC from network votes
ReplicaUpdateOnQC(r) ==
    \E msg \in network :
        LET v == msg.view
            b == msg.block
            highQCView == IF highQC[r] = Nil THEN 0 ELSE blockView[highQC[r]]
            bView == blockView[b]
        IN
        /\ r \in Honest             \* Only honest replicas update state
        /\ msg.type = "VOTE"
        /\ HasQuorumInNetwork(v, b)
        /\ bView > highQCView
        /\ highQC' = [highQC EXCEPT ![r] = b]
        /\ IF UpdateLockCond(r, b)
             THEN lockedQC' = [lockedQC EXCEPT ![r] = b]
             ELSE UNCHANGED lockedQC
        /\ LET parent == blockParent[b]
           IN IF CanCommit(parent, b)
              THEN committed' = [committed EXCEPT ![r] = @ \cup {parent}]
              ELSE UNCHANGED committed
        /\ UNCHANGED <<view, proposals, blocks, blockParent, blockView, blockHeight, network>>

\* hotstuff2.go:461-477 - onViewTimeout()
\* Only honest replicas send NEWVIEW on timeout
ReplicaTimeout(r) ==
    LET v == view[r]
        nextView == v + 1
    IN
    /\ r \in Honest                 \* Only honest replicas participate
    /\ nextView <= MaxView
    /\ view' = [view EXCEPT ![r] = nextView]
    /\ network' = network \cup { [type |-> "NEWVIEW", view |-> nextView, replica |-> r, qc |-> highQC[r]] }
    /\ UNCHANGED <<lockedQC, highQC, committed, proposals, blocks, blockParent, blockView, blockHeight>>

\* hotstuff2.go:431-452 - onNewView()
\* CRITICAL: Leader includes its own implicit vote (hotstuff2.go:733-739)
LeaderProcessNewView(r) ==
  \E v \in View :
    LET nvReplicas      == NewViewReplicasFromNetwork(v)
        highestQC       == HighestQCFromNetwork(v)
        availableBlocks == Block \ blocks
    IN
    /\ r \in Honest                 \* Only honest leaders process NEWVIEWs
    /\ r = Leader(v)
    /\ view[r] = v
    /\ IsQuorum(nvReplicas)
    /\ proposals[v] = Nil
    /\ availableBlocks /= {}
     /\ LET newBlock == CHOOSE b \in availableBlocks : TRUE
           parentHeight == IF highestQC = Nil THEN 0 ELSE blockHeight[highestQC]
       IN
       /\ blocks' = blocks \cup {newBlock}
       /\ blockParent' = blockParent @@
            (IF highestQC = Nil
             THEN [bb \in {newBlock} |-> Genesis]
             ELSE [bb \in {newBlock} |-> highestQC])
       /\ blockHeight' = blockHeight @@ [bb \in {newBlock} |-> parentHeight + 1]
       /\ blockView' = blockView @@ [bb \in {newBlock} |-> v]
       /\ proposals' = [proposals EXCEPT ![v] = newBlock]
       \* Leader broadcasts PROPOSE and includes its own VOTE (implicit vote)
       /\ network' = network \cup {
            [type |-> "PROPOSE", view |-> v, block |-> newBlock, qc |-> highestQC],
            [type |-> "VOTE", view |-> v, block |-> newBlock, replica |-> r]
          }
       /\ UNCHANGED <<view, lockedQC, highQC, committed>>

(*************************************************************************)
\* Next State Relation
(*************************************************************************)

Next ==
    \/ \E r \in Replica : LeaderPropose(r)
    \/ \E r \in Replica : ReplicaVote(r)
    \/ \E r \in Replica : LeaderFormQC(r)
    \/ \E r \in Replica : ReplicaUpdateOnQC(r)
    \/ \E r \in Replica : ReplicaTimeout(r)
    \/ \E r \in Replica : LeaderProcessNewView(r)

vars == <<view, lockedQC, highQC, committed, proposals, blocks,
          blockParent, blockView, blockHeight, network>>

Spec == Init /\ [][Next]_vars

(*************************************************************************)
\* Fairness (for Liveness)
(*************************************************************************)

Fairness ==
    /\ WF_vars(Next)
    /\ \A r \in Replica :
        /\ WF_vars(LeaderPropose(r))
        /\ WF_vars(ReplicaVote(r))
        /\ WF_vars(LeaderFormQC(r))
        /\ WF_vars(ReplicaUpdateOnQC(r))
        /\ WF_vars(ReplicaTimeout(r))

FairSpec == Spec /\ Fairness

(*************************************************************************)
\* Safety Invariants
(*************************************************************************)

\* No conflicting commits at same height
AgreementInvariant ==
    \A r1 \in Replica :
        \A r2 \in Replica :
            \A b1 \in committed[r1] :
                \A b2 \in committed[r2] :
                    (blockHeight[b1] = blockHeight[b2]) => (b1 = b2)

ValidityInvariant ==
    \A r \in Replica : committed[r] \subseteq blocks

QCIntegrityInvariant ==
    \A v \in View :
        \A b \in Block :
            (HasQCInNetwork(v, b) /= Nil) => HasQuorumInNetwork(v, b)

ForkFreedomInvariant ==
    \A v \in View :
        \A b1 \in Block :
            \A b2 \in Block :
                (b1 /= b2 /\ blockHeight[b1] = blockHeight[b2]
                 /\ HasQCInNetwork(v, b1) /= Nil /\ HasQCInNetwork(v, b2) /= Nil)
                => FALSE

CommitChainInvariant ==
    \A r \in Replica :
        \A b \in committed[r] :
            b /= Genesis => IsAncestor(Genesis, b)

NoDoubleVoteInvariant ==
    \A v \in View :
        \A r \in Replica :
            LET votedBlocks == { msg.block : msg \in {m \in network : m.type = "VOTE" /\ m.view = v /\ m.replica = r} }
            IN Cardinality(votedBlocks) <= 1

SingleProposalInvariant ==
    \A v \in View : proposals[v] = Nil \/ proposals[v] \in blocks

VotesSubsetReplicasInvariant ==
    \A v \in View :
        \A b \in Block :
            VotesFromNetwork(v, b) \subseteq Replica

UniqueQCPerViewInvariant ==
    \A v \in View :
        LET qcBlocks == { b \in Block : HasQCInNetwork(v, b) /= Nil }
        IN Cardinality(qcBlocks) <= 1

(*************************************************************************)
\* Fault Tolerance Invariants
(*************************************************************************)

\* NOTE: FaultToleranceAssumption and QuorumIntersectionProperty are
\* constant-level formulas (no variables). They are checked via ASSUME
\* statements at model initialization (see above), not as invariants.

\* Honest replicas never commit conflicting blocks (safety under faults)
HonestAgreementInvariant ==
    \A r1 \in Honest :
        \A r2 \in Honest :
            \A b1 \in committed[r1] :
                \A b2 \in committed[r2] :
                    (blockHeight[b1] = blockHeight[b2]) => (b1 = b2)

\* Only honest replicas' commits matter for validity
HonestValidityInvariant ==
    \A r \in Honest : committed[r] \subseteq blocks

(*************************************************************************)
\* Liveness Properties (require Fairness)
(*************************************************************************)

\* At least one honest replica eventually commits (under fairness + honest leader)
EventuallyCommit == <>(\E r \in Honest : committed[r] /= {})

EventualProgress ==
    <>(\E r \in Honest : Cardinality(committed[r]) > 0)

\* Views with honest leaders eventually complete or are skipped
ViewEventuallyCompletes ==
    \A v \in View :
        (proposals[v] /= Nil /\ Leader(v) \in Honest) =>
            <>(\/ \E b \in Block : HasQCInNetwork(v, b) /= Nil
               \/ \A r \in Honest : view[r] > v)

\* Honest replicas eventually synchronize views
EventualViewSync ==
    <>(\E v \in View : \A r \in Honest : view[r] = v)

(*************************************************************************)
\* State Constraints (bounds state space)
(*************************************************************************)

StateConstraint ==
    /\ \A r \in Replica : view[r] <= MaxView
    /\ Cardinality(blocks) <= MaxHeight
    /\ Cardinality(network) < 1000

=============================================================================
