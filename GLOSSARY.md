# Market data ingestion

Ingests the Binance aggregated trade stream and measures how fast each message goes through the service, against a latency SLO.

## Language

**Feed**:
The continuous stream of aggregated trades for one symbol from one venue.
_Avoid_: Stream (except for the Binance product name), flow

**Message**:
One unit received from the feed, before it is parsed.
_Avoid_: Event, packet

**Trade**:
A message successfully parsed and converted into typed values.
_Avoid_: aggTrade (outside the parser), order

**Drop**:
A message discarded on purpose because the buffer was full (backpressure).
_Avoid_: Loss, error

**Stage**:
A named, timed step a message goes through inside the service.

**Freshness**:
How old a message is according to the venue's event time. A signal, never an SLO basis, because the clocks are not synchronised.
_Avoid_: Latency, end-to-end latency

**Objective**:
The latency threshold the SLO commits to (p99 under 2 ms over 30 days).
_Avoid_: Target, SLA

**Warning tier**:
A stricter threshold (0.5 ms) that warns before the objective is at risk.

**Error budget**:
How long, over the SLO window, the objective may be missed without breaking the SLO.

**Admitted message**:
A message that entered the buffer, i.e. was not dropped.
_Avoid_: Accepted, processed

**Malformed message**:
A message that cannot be decoded as JSON.
_Avoid_: Parse error, JSON error

**Invalid trade**:
A decoded message whose fields (price, quantity, time) cannot be converted into typed values.
_Avoid_: Trade parse error, conversion error

**Gap**:
A period of feed time for which no message was received, such as during a reconnection or a restart.
_Avoid_: Data loss, outage

**Pipeline latency**:
The time from reading a message off the socket to it becoming a trade. The SLO is defined on it.
_Avoid_: Processing latency, end-to-end latency

**Queue wait**:
The part of pipeline latency a message spends waiting in the buffer.
_Avoid_: Buffer latency

**Processing time**:
The part of pipeline latency spent parsing and converting a message.
_Avoid_: Pipeline latency