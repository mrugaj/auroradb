
# AuroraDB

> A lightweight, purely relational embedded database engine written entirely from scratch in Go.

AuroraDB is a learning project that turned into a serious deep-dive into database internals. It implements a Copy-on-Write B+Tree, custom memory-mapped I/O, optimistic MVCC transactions, and a recursive-descent SQL-like query parser—without leaning on heavy external dependencies.

## Features

- **Copy-on-Write B+Tree Engine** — Native B+Tree implementation handling splits, merges, and leaf updates completely disk-backed and memory-safe.
- **ACID Transactions** — Multi-key atomic transactions with optimistic MVCC for race-condition prevention.
- **SQL-like Query Language** — Recursive-descent parser supporting table creation, inserts, updates, deletes, and advanced `SELECT` scans with filters, projections, and indexed range queries.
- **Secondary Indexes** — Automatic primary-key prefixing and multi-column indexed range scans.
- **Zero-Friction mmap Storage** — File segments mapped strictly to RAM via system calls, minimizing I/O overhead. Fragmented pages are recycled through a native unrolled linked-list freelist.

## Project Structure

```
auroradb/
├── cmd/         # Interactive CLI entrypoint
├── db/          # High-level DB abstraction and transactional KV wrapper
├── engine/      # Query planner and execution engine
├── index/       # Secondary indexing logic
├── parser/      # Recursive-descent tokenizer and AST parser
├── storage/     # B+Tree, KV store, freelist, and OS-level mmap
├── types/       # Schema definitions, encoding, and serialization
└── util/        # Helper data structures (SHA-256 backed Set, etc.)
```

## Installation

```bash
git clone https://github.com/mrugaj/auroradb.git
cd auroradb
go mod tidy
go build -o auroradb ./cmd/auroradb.go
```

## Usage

Start the interactive CLI:

```bash
./auroradb
```

```text
Starting AuroraDB Interactive CLI...
Example: open mydb.db;
aurora>> open mydb.db;
Successfully connected.
aurora>> create table users (id int, name bytes, index (name), primary key (id));
aurora>> insert into users (id, name) values (1, alice);
aurora>> select * from users;
+----+-------+
| ID | NAME  |
+----+-------+
| 1  | alice |
+----+-------+
aurora>> exit;
```

## Query Language

AuroraDB uses a SQL-like syntax. Note that string literals are currently parsed as bare tokens (quotes are optional in most contexts).

### Create Table

```sql
-- Multi-line
create table users (
    id int,
    name bytes,
    index (name),
    primary key (id)
);

-- Single-line
create table users (id int, name bytes, index (name), primary key (id));
```

### Insert

```sql
insert into users (id, name) values (1, alice);
insert into users (id, name) values (2, bob);
```

### Select

```sql
-- Full table scan
select * from users;

-- Filtered scan
select name, id from users filter id >= 1;

-- Indexed range scan
select * from users index by id >= 1 and id <= 10;
```

### Update

> **Note:** The current parser requires column names in `SET` clauses to be prefixed with `@`.

```sql
update users set @name = bob filter id == 1;
```

### Delete

```sql
delete from users filter id == 1;

-- Or using an index
delete from users index by name == alice;
```

## Architecture Highlights

### Copy-on-Write B+Tree
All tree mutations produce new nodes rather than modifying pages in-place. This makes rollback trivial and provides the foundation for MVCC.

### Optimistic MVCC
Transactions maintain read and write sets. Conflicts are detected at commit time; if a record has changed since it was read, the transaction aborts.

### Custom mmap & Freelist
Instead of a traditional buffer pool, AuroraDB maps file regions directly into process memory. Deleted pages are tracked in an unrolled linked list embedded in the database file itself, allowing immediate reuse without external allocation overhead.


## Why Build This?

The best way to understand how databases work is to build one. AuroraDB is primarily a learning project—don't run your production workload on it—but the process of implementing B+Trees, mmap, and query planning from scratch teaches you more about storage engines than any textbook.

## Acknowledgements

This following project has been inspired from the "Build Your Own Database From Scratch in Go" guide. 

## License

MIT
