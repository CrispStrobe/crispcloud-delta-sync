<?php

declare(strict_types=1);

namespace OCA\CrispCloudDelta\Controller;

use OCA\CrispCloudDelta\Service\BlockMapService;
use OCP\AppFramework\Controller;
use OCP\AppFramework\Http;
use OCP\AppFramework\Http\JSONResponse;
use OCP\IRequest;
use OCP\IUserSession;

class DeltaController extends Controller {
    private BlockMapService $blockMapService;
    private IUserSession $userSession;

    public function __construct(
        string $appName,
        IRequest $request,
        BlockMapService $blockMapService,
        IUserSession $userSession
    ) {
        parent::__construct($appName, $request);
        $this->blockMapService = $blockMapService;
        $this->userSession = $userSession;
    }

    private function getUserId(): ?string {
        $user = $this->userSession->getUser();
        return $user ? $user->getUID() : null;
    }

    /**
     * @NoAdminRequired
     * @NoCSRFRequired
     *
     * GET /api/blockmap/{path}
     */
    public function getBlockMap(string $path): JSONResponse {
        $userId = $this->getUserId();
        if ($userId === null) {
            return new JSONResponse(['error' => 'Not authenticated'], Http::STATUS_UNAUTHORIZED);
        }

        $blockMap = $this->blockMapService->getBlockMap($userId, '/' . $path);

        if ($blockMap === null) {
            return new JSONResponse(
                ['error' => 'File not found or not a regular file'],
                Http::STATUS_NOT_FOUND
            );
        }

        return new JSONResponse($blockMap);
    }

    /**
     * @NoAdminRequired
     * @NoCSRFRequired
     *
     * POST /api/blocks/{path}?offset=N&size=M
     */
    public function putBlock(string $path): JSONResponse {
        $userId = $this->getUserId();
        if ($userId === null) {
            return new JSONResponse(['error' => 'Not authenticated'], Http::STATUS_UNAUTHORIZED);
        }

        $offset = (int)$this->request->getParam('offset', '0');
        $size = (int)$this->request->getParam('size', '0');

        $data = file_get_contents('php://input');
        if ($data === false || strlen($data) === 0) {
            return new JSONResponse(['error' => 'Empty request body'], Http::STATUS_BAD_REQUEST);
        }

        if ($size > 0 && strlen($data) !== $size) {
            return new JSONResponse(
                ['error' => "Size mismatch: expected $size, got " . strlen($data)],
                Http::STATUS_BAD_REQUEST
            );
        }

        try {
            $this->blockMapService->writeBlock($userId, '/' . $path, $offset, $data);
        } catch (\Throwable $e) {
            return new JSONResponse(['error' => $e->getMessage()], Http::STATUS_INTERNAL_SERVER_ERROR);
        }

        return new JSONResponse(['status' => 'ok', 'offset' => $offset, 'size' => strlen($data)]);
    }

    /**
     * @NoAdminRequired
     * @NoCSRFRequired
     *
     * POST /api/finalize/{path}?size=N
     */
    public function finalize(string $path): JSONResponse {
        $userId = $this->getUserId();
        if ($userId === null) {
            return new JSONResponse(['error' => 'Not authenticated'], Http::STATUS_UNAUTHORIZED);
        }

        $sizeParam = $this->request->getParam('size');
        $newSize = ($sizeParam !== null) ? (int)$sizeParam : -1;

        try {
            $this->blockMapService->finalizeFile($userId, '/' . $path, $newSize);
        } catch (\Throwable $e) {
            return new JSONResponse(['error' => $e->getMessage()], Http::STATUS_INTERNAL_SERVER_ERROR);
        }

        return new JSONResponse(['status' => 'finalized']);
    }

    /**
     * @NoAdminRequired
     * @NoCSRFRequired
     * @PublicPage
     *
     * GET /api/status
     */
    public function status(): JSONResponse {
        return new JSONResponse([
            'app' => 'crispcloud_delta',
            'version' => '0.1.0',
            'status' => 'ok',
            'blockSize' => 4 * 1024 * 1024,
            'algorithm' => 'adler32+sha256',
        ]);
    }
}
