<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta\AppInfo;

use OCA\CrispCloudDelta\Capabilities;
use OCP\AppFramework\App;
use OCP\AppFramework\Bootstrap\IBootContext;
use OCP\AppFramework\Bootstrap\IBootstrap;
use OCP\AppFramework\Bootstrap\IRegistrationContext;

class Application extends App implements IBootstrap {
    public const APP_ID = 'crispcloud_delta';

    public function __construct() {
        parent::__construct(self::APP_ID);
    }

    public function register(IRegistrationContext $context): void {
        $context->registerCapability(Capabilities::class);
    }

    public function boot(IBootContext $context): void {
    }
}
