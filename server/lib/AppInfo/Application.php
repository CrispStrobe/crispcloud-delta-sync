<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta\AppInfo;

use OCA\CrispCloudDelta\Capabilities;
use OCP\AppFramework\App;

// IBootstrap was introduced in Nextcloud 20 and does not exist in ownCloud 10.
// Using the old-style container registration keeps the app compatible with both.
class Application extends App {
    public const APP_ID = 'crispcloud_delta';

    public function __construct() {
        parent::__construct(self::APP_ID);
        $this->getContainer()->registerCapability(Capabilities::class);
    }
}
