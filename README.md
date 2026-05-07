
# AuroraDB

AuroraDB is a lightweight, purely relational, embedded database engine written entirely from scratch in Go. It implements advanced database principles like a Copy-on-Write (CoW) B+Tree, custom Memory Mapped IO (mmap), Multi-Version Concurrency Control (MVCC) transactional abstractions, and a full SQL-like query language natively without relying on heavy external dependencies.

## Key Features

- **Copy-on-Write B+Tree Engine:** Native B+Tree handling splits, merges, and leaf node updates completely disk-backed and memory-safe.
- **ACID Transactions:** Full support for multi-key atomic transactions and preventing race conditions via optimistic MVCC.
- **SQL-like Execution:** Built-in recursive-descent query language parser that handles table creation, inserts, and highly advanced `SELECT` scans, filters, and projections.
- **Secondary Indexes:** Supports automatic primary-key prefixing and multi-column indexed range scans.
- **Zero-Friction Mmap Storage:** Maps file descriptor segments strictly to RAM via system calls, drastically reducing IO overhead. Reuses and recycles fragmented pages effectively with an unrolled linked list `freelist` mechanism built natively over the mapped slices.

## Project Structure

- `cmd/`: Command line interface (CLI) to easily interact with the AuroraDB instances.
- `db/`: The higher-level database abstraction tying KV operations to database tables.
- `engine/`: The core execution engine matching parsed queries to optimal indexed routes.
- `index/`: Extracts secondary indexing logic.
- `parser/`: Recursive descent SQL-like query tokenizer and AST tree parser.
- `storage/`: The lower-level system containing the `B+Tree` implementation, KV store, Freelist tracking, and OS-level `mmap` interfaces.
- `types/`: Universal encoding, schema logic, and serialization primitives.
- `util/`: Helper data structures like the SHA-256 backed `Set`.

## Getting Started

1. Clone the repository
2. Tidy up the Go modules:
   ```bash
   go mod tidy