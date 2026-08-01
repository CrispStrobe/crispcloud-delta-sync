<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta\Service;

final class EtagMismatchException extends \RuntimeException {
    public function __construct(string $path, string $expected, string $actual) {
        parent::__construct("ETag precondition failed for $path (expected $expected, current $actual)");
    }
}
