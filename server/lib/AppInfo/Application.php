<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta\AppInfo;

use OCA\CrispCloudDelta\Capabilities;
use OCP\AppFramework\App;
use OCP\ILogger;

// IBootstrap was introduced in Nextcloud 20 and does not exist in ownCloud 10.
// Using the old-style container registration keeps the app compatible with both.
class Application extends App {
    public const APP_ID = 'crispcloud_delta';

    public function __construct() {
        parent::__construct(self::APP_ID);
        $container = $this->getContainer();
        $container->registerCapability(Capabilities::class);
        // OCP\ILogger is deprecated in NC 33 and no longer auto-resolvable;
        // ownCloud 10 also never auto-injects it. Register explicitly for both.
        $container->registerService(ILogger::class, function () {
            return \OC::$server->getLogger();
        });
    }
}
