<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta;

use OCP\Capabilities\ICapability;

class Capabilities implements ICapability {
    public function getCapabilities(): array {
        return [
            'crispcloud_delta' => [
                'enabled' => true,
                'version' => '0.1.0',
                'blockSize' => 4 * 1024 * 1024,
                'algorithm' => 'adler32+sha256',
            ],
        ];
    }
}
