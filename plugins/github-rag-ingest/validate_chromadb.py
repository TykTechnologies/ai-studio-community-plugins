#!/usr/bin/env python3
"""
Validate GitHub RAG Plugin chunks in ChromaDB
"""

import chromadb
from chromadb.config import Settings
import json

def main():
    # Connect to ChromaDB
    print("Connecting to ChromaDB at http://localhost:8000...")
    client = chromadb.HttpClient(
        host="localhost",
        port=8000,
        settings=Settings(allow_reset=True, anonymized_telemetry=False)
    )

    # List all collections
    print("\n📋 All Collections:")
    collections = client.list_collections()
    for coll in collections:
        print(f"  - {coll.name} ({coll.count()} documents)")

    # Check the "test" collection in detail
    print(f"\n🔍 Inspecting 'test' collection metadata...")

    try:
        collection = client.get_collection(name="test")
        sample = collection.get(limit=20, include=["metadatas", "documents"])

        print(f"\n📊 Sample metadata from 'test' collection:")

        if sample['metadatas']:
            # Look at recent documents (might be at the end)
            start_idx = max(0, len(sample['metadatas']) - 5)
            for i in range(start_idx, len(sample['metadatas'])):
                metadata = sample['metadatas'][i]
                doc_preview = sample['documents'][i][:100] if sample['documents'] else ""

                print(f"\n  Document {i+1}:")
                print(f"    Content: {doc_preview}...")
                print(f"    Metadata keys: {list(metadata.keys()) if metadata else 'None'}")

                if metadata:
                    # Check if it's GitHub data (look for repo_ or file_ keys)
                    github_keys = [k for k in metadata.keys() if k.startswith(('repo_', 'file_', 'github_', 'commit_'))]
                    if github_keys:
                        print(f"    🎯 GITHUB DATA FOUND!")
                        for key in github_keys:
                            print(f"      {key}: {metadata[key]}")

            # Search specifically for metadata
            print(f"\n🔍 Searching for recent GitHub ingestion...")
            print(f"   Looking for metadata with 'repo_name' or 'file_path' keys...")

            found_github = False
            for metadata in sample['metadatas']:
                if metadata and ('repo_name' in metadata or 'file_path' in metadata or 'repo_id' in metadata):
                    found_github = True
                    print(f"\n   ✅ Found GitHub metadata!")
                    print(f"   Sample metadata: {json.dumps(metadata, indent=6)}")
                    break

            if not found_github:
                print(f"\n   ❌ No GitHub metadata found in sample")
                print(f"\n   Sample of existing metadata structure:")
                if sample['metadatas'] and sample['metadatas'][0]:
                    print(f"   {json.dumps(sample['metadatas'][0], indent=6)}")

        return

    except Exception as e:
        print(f"\n❌ Error inspecting collection: {e}")

    # Now analyze the collection with GitHub data
    try:
        collection = client.get_collection(name=collection_name)
        print(f"\n✅ Found collection: {collection_name}")
        print(f"   Total documents: {collection.count()}")

        # Get sample documents
        print(f"\n📄 Sample Documents (first 5):")
        results = collection.get(
            limit=5,
            include=["metadatas", "documents"]
        )

        if results['ids']:
            for i, doc_id in enumerate(results['ids']):
                metadata = results['metadatas'][i] if results['metadatas'] else {}
                document = results['documents'][i] if results['documents'] else ""

                print(f"\n  Document {i+1}:")
                print(f"    ID: {doc_id}")
                print(f"    Content preview: {document[:100]}...")
                print(f"    Metadata:")
                for key, value in metadata.items():
                    print(f"      {key}: {value}")

        # Query for specific content
        print(f"\n🔍 Semantic Search Test (query: 'AI Studio'):")
        query_results = collection.query(
            query_texts=["AI Studio"],
            n_results=3,
            include=["metadatas", "documents", "distances"]
        )

        if query_results['ids'] and query_results['ids'][0]:
            for i, doc_id in enumerate(query_results['ids'][0]):
                distance = query_results['distances'][0][i] if query_results['distances'] else None
                metadata = query_results['metadatas'][0][i] if query_results['metadatas'] else {}
                document = query_results['documents'][0][i] if query_results['documents'] else ""

                print(f"\n  Result {i+1}:")
                print(f"    Distance: {distance:.4f}")
                print(f"    File: {metadata.get('file_path', 'N/A')}")
                print(f"    GitHub URL: {metadata.get('github_url', 'N/A')}")
                print(f"    Lines: {metadata.get('line_start', 'N/A')}-{metadata.get('line_end', 'N/A')}")
                print(f"    Content preview: {document[:150]}...")
        else:
            print("  No results found")

        # Check metadata distribution
        print(f"\n📊 Metadata Statistics:")
        sample_large = collection.get(
            limit=100,
            include=["metadatas"]
        )

        if sample_large['metadatas']:
            repos = set()
            files = set()
            file_types = {}

            for metadata in sample_large['metadatas']:
                if 'repo_name' in metadata:
                    repos.add(metadata['repo_name'])
                if 'file_path' in metadata:
                    files.add(metadata['file_path'])
                if 'file_type' in metadata:
                    file_type = metadata['file_type']
                    file_types[file_type] = file_types.get(file_type, 0) + 1

            print(f"  Unique repositories: {len(repos)} ({', '.join(repos)})")
            print(f"  Unique files: {len(files)}")
            print(f"  File types:")
            for ftype, count in sorted(file_types.items(), key=lambda x: x[1], reverse=True):
                print(f"    {ftype}: {count} chunks")

        print(f"\n✅ Validation complete!")

    except Exception as e:
        print(f"\n❌ Error accessing collection '{collection_name}': {e}")
        print("\nMake sure:")
        print("  1. ChromaDB is running on localhost:8000")
        print("  2. The collection name matches your repository namespace")
        print("  3. Ingestion job completed successfully")

if __name__ == "__main__":
    main()
