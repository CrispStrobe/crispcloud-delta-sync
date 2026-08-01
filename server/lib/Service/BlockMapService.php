<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta\Service;

use OCP\Files\IRootFolder;
use OCP\Files\NotFoundException;
use OCP\IConfig;

/**
 * Computes, caches, and serves block-level file indexes.
 *
 * Block map format (JSON):
 * {
 *   "filePath": "/path/to/file",
 *   "totalSize": 104857600,
 *   "blockSize": 4194304,
 *   "blockCount": 25,
 *   "signatures": [
 *     {"blockIndex": 0, "offset": 0, "size": 4194304, "weakHash": 12345, "strongHash": "abcdef..."},
 *     ...
 *   ],
 *   "createdAt": "2026-06-08T12:00:00Z",
 *   "etag": "abc123"
 * }
 */
class BlockMapService {
    private const BLOCK_SIZE = 4 * 1024 * 1024; // 4 MB
    private const ADLER_MOD = 65521;
    private const CACHE_FOLDER = '.crispcloud_delta';
    private const STAGING_FOLDER = '.crispcloud_delta/staging';
    private const FINALIZE_LOCK_RETRIES = 200;
    private const FINALIZE_LOCK_RETRY_US = 100000;

    private IRootFolder $rootFolder;
    private IConfig $config;

    public function __construct(
        IRootFolder $rootFolder,
        IConfig $config
    ) {
        $this->rootFolder = $rootFolder;
        $this->config = $config;
    }

    /**
     * Get or compute the block map for a file.
     *
     * Returns cached map if the file's ETag hasn't changed.
     * Recomputes and caches if stale or missing.
     */
    public function getBlockMap(string $userId, string $path): ?array {
        $userFolder = $this->rootFolder->getUserFolder($userId);

        try {
            $file = $userFolder->get($path);
        } catch (NotFoundException $e) {
            return null;
        }

        if ($file->getType() !== \OCP\Files\FileInfo::TYPE_FILE) {
            return null;
        }

        $etag = $file->getEtag();
        $fileSize = $file->getSize();

        // Check cache
        $cached = $this->loadCachedBlockMap($userId, $path);
        if ($cached !== null && isset($cached['etag']) && $cached['etag'] === $etag) {
            return $cached;
        }

        // Compute fresh block map
        error_log("crispcloud_delta: computing block map for $path ($fileSize bytes)");

        $blockMap = $this->computeBlockMap($file, $path);
        $blockMap['etag'] = $etag;

        // Cache it
        $this->saveCachedBlockMap($userId, $path, $blockMap);

        return $blockMap;
    }

    /**
     * Compute the block map for a Nextcloud file node.
     */
    private function computeBlockMap(\OCP\Files\File $file, string $path): array {
        $size = $file->getSize();
        $blockSize = self::BLOCK_SIZE;
        $blockCount = $size === 0 ? 0 : (int)ceil($size / $blockSize);
        $signatures = [];

        $handle = $file->fopen('rb');
        if ($handle === false) {
            throw new \RuntimeException("Cannot open file: $path");
        }

        try {
            for ($i = 0; $i < $blockCount; $i++) {
                $offset = $i * $blockSize;
                $remaining = min($blockSize, $size - $offset);
                $data = '';
                // fread may return short reads — loop until we have the full block
                while (strlen($data) < $remaining) {
                    $chunk = fread($handle, $remaining - strlen($data));
                    if ($chunk === false || $chunk === '') {
                        break;
                    }
                    $data .= $chunk;
                }
                if (strlen($data) === 0) {
                    break;
                }
                $actualSize = strlen($data);

                $signatures[] = [
                    'blockIndex' => $i,
                    'offset' => $offset,
                    'size' => $actualSize,
                    'weakHash' => $this->adler32($data),
                    'strongHash' => hash('sha256', $data),
                ];
            }
        } finally {
            fclose($handle);
        }

        return [
            'filePath' => $path,
            'totalSize' => $size,
            'blockSize' => $blockSize,
            'blockCount' => $blockCount,
            'signatures' => $signatures,
            'createdAt' => (new \DateTimeImmutable())->format(\DateTimeInterface::ATOM),
        ];
    }

    /**
     * Adler-32 checksum (RFC 1950) — must match the Dart implementation.
     */
    private function adler32(string $data): int {
        $a = 1;
        $b = 0;
        $len = strlen($data);

        for ($i = 0; $i < $len; $i++) {
            $a = ($a + ord($data[$i])) % self::ADLER_MOD;
            $b = ($b + $a) % self::ADLER_MOD;
        }

        return ($b << 16) | $a;
    }

    /**
     * Write a block of data at a specific offset in a file.
     */
    public function writeBlock(
        string $userId,
        string $path,
        int $offset,
        string $data,
        ?string $ifMatch = null
    ): void {
        if ($offset < 0) {
            throw new \InvalidArgumentException("Negative block offset: $offset");
        }
        $this->withPathLock($userId, $path, function () use ($userId, $path, $offset, $data, $ifMatch): void {
            $this->assertEtag($userId, $path, $ifMatch);
            $userFolder = $this->rootFolder->getUserFolder($userId);
            $file = $userFolder->get($path);
            if ($file->getType() !== \OCP\Files\FileInfo::TYPE_FILE) {
                throw new \RuntimeException("Not a file: $path");
            }

            $stageFolder = $this->getOrCreateFolder($userFolder, $this->stagePath($path));
            $name = (string)$offset;
            try {
                $stageFile = $stageFolder->get($name);
                $stageFile->putContent($data);
            } catch (NotFoundException $e) {
                // ownCloud 10's IFolder::newFile accepts only the name;
                // Nextcloud also supports the one-argument form. Write the
                // payload explicitly so both APIs stage identical bytes.
                $stageFile = $stageFolder->newFile($name);
                $stageFile->putContent($data);
            }
            error_log("crispcloud_delta: staged block at offset $offset (" . strlen($data) . " bytes) for $path");
        });
    }

    /**
     * Finalize a file after block writes — touch mtime and invalidate cache.
     */
    public function finalizeFile(
        string $userId,
        string $path,
        int $newSize = -1,
        ?string $ifMatch = null
    ): void {
        for ($attempt = 0; ; $attempt++) {
            try {
                $this->withPathLock($userId, $path, function () use ($userId, $path, $newSize, $ifMatch): void {
                    $this->assertEtag($userId, $path, $ifMatch);
                    $userFolder = $this->rootFolder->getUserFolder($userId);
                    $file = $userFolder->get($path);
                    $content = $file->getContent();
                    foreach ($this->readStagedBlocks($userFolder, $this->stagePath($path)) as $offset => $data) {
                        $end = $offset + strlen($data);
                        if ($end > strlen($content)) {
                            $content .= str_repeat("\0", $end - strlen($content));
                        }
                        $content = substr_replace($content, $data, $offset, strlen($data));
                    }

                    if ($newSize >= 0) {
                        if ($newSize < strlen($content)) {
                            $content = substr($content, 0, $newSize);
                        } elseif ($newSize > strlen($content)) {
                            $content .= str_repeat("\0", $newSize - strlen($content));
                        }
                    }

                    // A single storage operation keeps readers on the old file until
                    // the complete patched content is ready.
                    $file->putContent($content);
                    $file->touch();
                    $blockMap = $this->computeBlockMap($file, $path);
                    $blockMap['etag'] = $file->getEtag();
                    $this->saveCachedBlockMap($userId, $path, $blockMap);
                    $this->clearStagedBlocks($userFolder, $this->stagePath($path));
                    error_log("crispcloud_delta: finalized delta sync for $path");
                });
                return;
            } catch (EtagMismatchException $e) {
                throw $e;
            } catch (\Throwable $e) {
                $isLocked = stripos($e->getMessage(), 'locked') !== false;
                if (!$isLocked || $attempt >= self::FINALIZE_LOCK_RETRIES) {
                    throw $e;
                }
                usleep(self::FINALIZE_LOCK_RETRY_US);
            }
        }
    }

    /**
     * Enforce an optional HTTP If-Match validator before a mutation.
     *
     * The request controller passes the header through this method. The
     * nullable parameter keeps older clients fully backward compatible while
     * allowing newer clients to reject a stale block map with HTTP 412.
     */
    public function assertEtag(string $userId, string $path, ?string $ifMatch): void {
        if ($ifMatch === null || trim($ifMatch) === '') {
            return;
        }

        $userFolder = $this->rootFolder->getUserFolder($userId);
        $file = $userFolder->get($path);
        $actual = $file->getEtag();
        $expectedValues = array_map('trim', explode(',', $ifMatch));

        foreach ($expectedValues as $expected) {
            if ($expected === '*') {
                return;
            }
            $expected = preg_replace('/^W\\//i', '', $expected) ?? $expected;
            $expected = trim($expected, '"');
            if ($expected === $actual) {
                return;
            }
        }

        throw new EtagMismatchException($path, $ifMatch, $actual);
    }

    private function stagePath(string $path): string {
        return self::STAGING_FOLDER . '/' . hash('sha256', $path);
    }

    /** @return array<int, string> */
    private function readStagedBlocks($userFolder, string $stagePath): array {
        try {
            $folder = $userFolder->get($stagePath);
        } catch (NotFoundException $e) {
            return [];
        }
        $blocks = [];
        foreach ($folder->getDirectoryListing() as $node) {
            if ($node->getType() !== \OCP\Files\FileInfo::TYPE_FILE) {
                continue;
            }
            $offset = filter_var($node->getName(), FILTER_VALIDATE_INT);
            if ($offset === false || $offset < 0) {
                continue;
            }
            $blocks[(int)$offset] = $node->getContent();
        }
        ksort($blocks, SORT_NUMERIC);
        return $blocks;
    }

    private function clearStagedBlocks($userFolder, string $stagePath): void {
        try {
            $userFolder->get($stagePath)->delete();
        } catch (NotFoundException $e) {
            // Nothing staged is a valid no-op finalize.
        }
    }

    private function getOrCreateFolder($userFolder, string $path) {
        $current = $userFolder;
        foreach (explode('/', trim($path, '/')) as $part) {
            if ($part === '') {
                continue;
            }
            try {
                $current = $current->get($part);
            } catch (NotFoundException $e) {
                $current = $current->newFolder($part);
            }
        }
        return $current;
    }

    private function withPathLock(string $userId, string $path, callable $operation): void {
        $lockPath = sys_get_temp_dir() . '/crispcloud_delta_' . hash('sha256', $userId . ':' . $path) . '.lock';
        $lock = fopen($lockPath, 'c');
        if ($lock === false) {
            throw new \RuntimeException("Cannot open delta lock for $path");
        }
        try {
            if (!flock($lock, LOCK_EX)) {
                throw new \RuntimeException("Cannot lock delta path: $path");
            }
            $operation();
            flock($lock, LOCK_UN);
        } finally {
            fclose($lock);
        }
    }

    // -------------------------------------------------------------------------
    // Cache helpers
    // -------------------------------------------------------------------------

    private function getCachePath(string $userId, string $path): string {
        $hash = hash('sha256', $path);
        return self::CACHE_FOLDER . '/' . $hash . '.json';
    }

    private function loadCachedBlockMap(string $userId, string $path): ?array {
        $userFolder = $this->rootFolder->getUserFolder($userId);
        $cachePath = $this->getCachePath($userId, $path);

        try {
            $cacheFile = $userFolder->get($cachePath);
            $json = $cacheFile->getContent();
            return json_decode($json, true);
        } catch (NotFoundException $e) {
            return null;
        } catch (\Throwable $e) {
            error_log("crispcloud_delta: failed to load cached block map: " . $e->getMessage());
            return null;
        }
    }

    private function saveCachedBlockMap(string $userId, string $path, array $blockMap): void {
        $userFolder = $this->rootFolder->getUserFolder($userId);
        $cachePath = $this->getCachePath($userId, $path);

        try {
            // Ensure cache folder exists
            try {
                $userFolder->get(self::CACHE_FOLDER);
            } catch (NotFoundException $e) {
                $userFolder->newFolder(self::CACHE_FOLDER);
            }

            $json = json_encode($blockMap, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES);

            try {
                $cacheFile = $userFolder->get($cachePath);
                $cacheFile->putContent($json);
            } catch (NotFoundException $e) {
                $userFolder->newFile($cachePath, $json);
            }
        } catch (\Throwable $e) {
            error_log("crispcloud_delta: failed to save cached block map: " . $e->getMessage());
        }
    }
}
